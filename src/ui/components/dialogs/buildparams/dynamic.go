package buildparams

import (
	stdctx "context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

type resolveState uint8

const (
	resolveReady resolveState = iota
	resolveWaiting
	resolveLoading
	resolveDone
	resolveFailed
)

type FormLoadedMsg struct {
	DialogID uint64
	// Gen identifies the load; only the latest one is applied.
	Gen  uint64
	Form formResolver
	Err  error
}

type ChoicesLoadedMsg struct {
	DialogID uint64
	Param    string
	Gen      uint64
	Choices  []gojenkins.Choice
	Err      error
}

func (d *BuildParamsDialog) loadForm() tea.Cmd {
	d.formGen++
	gen := d.formGen
	return func() tea.Msg {
		ctx, cancel := d.requestContext()
		defer cancel()
		form, err := d.formLoader(ctx)
		return FormLoadedMsg{DialogID: d.id, Gen: gen, Form: form, Err: err}
	}
}

func (d *BuildParamsDialog) handleFormLoaded(msg FormLoadedMsg) tea.Cmd {
	if (msg.DialogID != 0 && msg.DialogID != d.id) || msg.Gen != d.formGen {
		return nil
	}
	if msg.Err != nil || msg.Form == nil {
		d.form = nil
		return d.degradeDynamicParams()
	}

	d.form = msg.Form
	formParams := make(map[string]gojenkins.FormParameter)
	for _, fp := range msg.Form.ParametersList() {
		formParams[fp.Name] = fp
	}
	var cmds []tea.Cmd
	for _, p := range d.params {
		if !isActiveChoiceParam(p.def.Type) {
			continue
		}
		fp, found := formParams[p.name()]
		if !found {
			cmds = append(cmds, d.degrade(p, "parameter was not found in the build form"))
			continue
		}
		if p.def.Type == classDynamicReference {
			p.omitValue = fp.OmitValueField
			reason := "dynamic reference parameters are not supported"
			if p.omitValue {
				reason = "display-only; not sent with the build"
			}
			cmds = append(cmds, d.degrade(p, reason))
			continue
		}
		d.installChoiceField(p, fp)
	}
	return tea.Batch(append(cmds, d.resolveReadyFields())...)
}

// installChoiceField replaces p's field with the form's choices, keeping the
// current value across a reload and falling back to the Jenkins default. A
// cascading parameter waits for its parents before it can be resolved.
func (d *BuildParamsDialog) installChoiceField(p *paramState, fp gojenkins.FormParameter) {
	value := p.field.Value()
	if value == "" && p.def.DefaultParameterValue.Value != nil {
		value = fmt.Sprintf("%v", p.def.DefaultParameterValue.Value)
	}
	field := newChoiceField(fp.Choices, fp.Multiple)
	p.filterable = fp.Filterable
	if value != "" {
		field.SetValue(value)
	}
	p.field = field
	if fp.Type == classCascadeChoice || len(fp.ReferencedParameters) > 0 {
		field.loading = true
		p.status = resolveWaiting
	} else {
		p.status = resolveDone
	}
}

func (d *BuildParamsDialog) handleChoicesLoaded(msg ChoicesLoadedMsg) tea.Cmd {
	if msg.DialogID != 0 && msg.DialogID != d.id {
		return nil
	}
	p := d.param(msg.Param)
	if p == nil || msg.Gen != p.gen {
		return nil
	}
	if msg.Err != nil {
		value := p.field.Value()
		focusCmd := d.degrade(p, shortResolveError(msg.Err))
		p.field.SetValue(value)
		return tea.Batch(focusCmd, d.resolveDependents(msg.Param))
	}
	field, ok := p.field.(*choiceField)
	if !ok {
		field = newChoiceField(nil, false)
		p.field = field
	}
	oldValue := field.Value()
	field.loading = false
	field.SetChoices(msg.Choices)
	p.status = resolveDone
	p.reason = ""
	if oldValue != field.Value() {
		return d.propagateChange(msg.Param)
	}
	return d.resolveReadyFields()
}

func (d *BuildParamsDialog) resolveReadyFields() tea.Cmd {
	var cmds []tea.Cmd
	for _, p := range d.params {
		if p.status != resolveWaiting || !d.parentsReady(p.name()) {
			continue
		}
		cmds = append(cmds, d.resolveField(p))
	}
	if len(cmds) == 0 && !d.anyLoading() {
		// Cyclic dependency groups can never become ready. Degrade them once.
		for _, p := range d.params {
			if p.status == resolveWaiting {
				cmds = append(cmds, d.degrade(p, "cyclic parameter dependency"))
			}
		}
	}
	return tea.Batch(cmds...)
}

func (d *BuildParamsDialog) resolveDependents(name string) tea.Cmd {
	if d.form == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, dependent := range d.form.Dependents(name) {
		p := d.param(dependent)
		if p != nil && p.status == resolveWaiting && d.parentsReady(dependent) {
			cmds = append(cmds, d.resolveField(p))
		}
	}
	return tea.Batch(cmds...)
}

func (d *BuildParamsDialog) resolveField(p *paramState) tea.Cmd {
	p.gen++
	p.status = resolveLoading
	if field, ok := p.field.(*choiceField); ok {
		field.loading = true
	}
	name, gen := p.name(), p.gen
	values := d.values()
	form := d.form
	resolve := func() tea.Msg {
		ctx, cancel := d.requestContext()
		defer cancel()
		choices, err := form.Resolve(ctx, name, values)
		return ChoicesLoadedMsg{DialogID: d.id, Param: name, Gen: gen, Choices: choices, Err: err}
	}
	return tea.Batch(resolve, d.startSpinner())
}

// startSpinner begins the loading animation when a field is pending and no
// tick chain is already running.
func (d *BuildParamsDialog) startSpinner() tea.Cmd {
	if d.spinning || !d.hasPendingResolution() {
		return nil
	}
	d.spinning = true
	return d.spinner.Tick
}

// handleSpinnerTick advances the animation, and ends the tick chain once
// every field has resolved or failed.
func (d *BuildParamsDialog) handleSpinnerTick(msg spinner.TickMsg) tea.Cmd {
	if !d.hasPendingResolution() {
		d.spinning = false
		return nil
	}
	var cmd tea.Cmd
	d.spinner, cmd = d.spinner.Update(msg)
	return cmd
}

func (d *BuildParamsDialog) propagateChange(name string) tea.Cmd {
	if d.form == nil {
		return nil
	}
	visited := map[string]bool{name: true}
	queue := []string{name}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, dependent := range d.form.Dependents(parent) {
			if visited[dependent] {
				continue
			}
			visited[dependent] = true
			queue = append(queue, dependent)
			p := d.param(dependent)
			if p == nil {
				continue
			}
			p.gen++
			p.status = resolveWaiting
			if field, ok := p.field.(*choiceField); ok {
				field.loading = true
			}
		}
	}
	return d.resolveDependents(name)
}

func (d *BuildParamsDialog) parentsReady(name string) bool {
	for _, fp := range d.form.ParametersList() {
		if fp.Name != name {
			continue
		}
		for _, parent := range fp.ReferencedParameters {
			if p := d.param(parent); p != nil && (p.status == resolveWaiting || p.status == resolveLoading) {
				return false
			}
		}
		return true
	}
	return false
}

func (d *BuildParamsDialog) anyLoading() bool {
	for _, p := range d.params {
		if p.status == resolveLoading {
			return true
		}
	}
	return false
}

func (d *BuildParamsDialog) hasPendingResolution() bool {
	for _, p := range d.params {
		if p.status == resolveLoading || p.status == resolveWaiting {
			return true
		}
	}
	return false
}

func (d *BuildParamsDialog) values() map[string]string {
	values := make(map[string]string, len(d.params))
	for _, p := range d.params {
		values[p.name()] = p.field.Value()
	}
	return values
}

func (d *BuildParamsDialog) requestContext() (stdctx.Context, stdctx.CancelFunc) {
	if d.program != nil {
		return d.program.RequestContext()
	}
	return stdctx.WithCancel(stdctx.Background())
}

func shortResolveError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, gojenkins.ErrNoChoices):
		return "no choices"
	case errors.Is(err, gojenkins.ErrStaleBuildForm):
		return "form expired; reload"
	}
	message := err.Error()
	for _, status := range []string{"HTTP 500", "HTTP 403", "HTTP 404"} {
		if strings.Contains(message, status) {
			return status
		}
	}
	if isTimeout(err) {
		return "request failed: timeout"
	}
	return "request failed"
}

func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return true
	}
	return errors.Is(err, stdctx.DeadlineExceeded) || errors.Is(err, http.ErrHandlerTimeout)
}
