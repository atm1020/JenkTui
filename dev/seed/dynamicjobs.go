package main

import "fmt"

// Active Choices (uno-choice) fixtures.
//
// The Jenkins REST API never reports the options of an Active Choices parameter:
// `.../api/json?depth=2` only exposes the class name (ChoiceParameter,
// CascadeChoiceParameter, DynamicReferenceParameter) and the default value, so
// JenkTui has to evaluate the choices through the plugin's own endpoints. These
// jobs exist to cover that work: deep cascades, parameters referencing several
// parents, every choice/reference render type, multi-value parents, filterable
// and oversized option lists, slow scripts, empty results and fallback scripts.
//
// `multi-input-release-flow` additionally branches its `input` gates on the
// parameter values, so one job produces different pending-input shapes
// (plain approval, input with its own parameters, two sequential approvals)
// depending on what was submitted.

// unoFallback is the Groovy script used whenever the main script throws.
const unoFallback = "return ['UNAVAILABLE']"

// unoChoiceOpts tunes a single uno-choice parameter definition.
type unoChoiceOpts struct {
	// choiceType is PT_SINGLE_SELECT, PT_MULTI_SELECT, PT_CHECKBOX or PT_RADIO
	// for choice parameters, and ET_TEXT_BOX, ET_FORMATTED_HTML,
	// ET_ORDERED_LIST or ET_UNORDERED_LIST for reference parameters.
	choiceType string
	// referenced is the comma-separated list of parameters this one cascades on.
	referenced       string
	filterable       bool
	visibleItemCount int
	// fallback overrides unoFallback; used to exercise the fallback path.
	fallback string
	// omitValueField applies to reference parameters that only render markup.
	omitValueField bool
}

func (o unoChoiceOpts) withDefaults(choiceType string) unoChoiceOpts {
	if o.choiceType == "" {
		o.choiceType = choiceType
	}
	if o.visibleItemCount == 0 {
		o.visibleItemCount = 1
	}
	if o.fallback == "" {
		o.fallback = unoFallback
	}
	return o
}

// unoScriptXML renders the shared GroovyScript block. The fallback element must
// be `secureFallbackScript`: GroovyScript keeps `fallbackScript` only as a
// transient legacy string, so a `<fallbackScript>` element deserializes into an
// unapproved, empty script and every evaluation fails with "script not yet
// approved for use".
func unoScriptXML(script, fallback string) string {
	return fmt.Sprintf(`<script class="org.biouno.unochoice.model.GroovyScript">
  <secureScript plugin="script-security">
    <script>%s</script>
    <sandbox>true</sandbox>
  </secureScript>
  <secureFallbackScript plugin="script-security">
    <script>%s</script>
    <sandbox>true</sandbox>
  </secureFallbackScript>
</script>`, xmlEscape(script), xmlEscape(fallback))
}

// cascadeStateXML is the empty state map every cascadable parameter needs.
//
// `parameters` is a non-transient final Map on AbstractCascadableParameter and
// the plugin has no readResolve, so a config.xml missing this element
// deserializes it as null. The parameter then renders, but the browser's
// cascade callback (doUpdate) throws a NullPointerException and the dependent
// select stays empty. Jenkins itself always writes `<parameters
// class="linked-hash-map"/>`, so the seeded XML has to as well.
const cascadeStateXML = `<parameters class="linked-hash-map"/>`

// unoChoice builds a ChoiceParameter / CascadeChoiceParameter definition.
// Element order follows what Jenkins serializes for these types.
func unoChoice(name, description, script string, opts unoChoiceOpts) paramDef {
	opts = opts.withDefaults("PT_SINGLE_SELECT")

	tag := "org.biouno.unochoice.ChoiceParameter"
	cascade := ""
	if opts.referenced != "" {
		tag = "org.biouno.unochoice.CascadeChoiceParameter"
		cascade = fmt.Sprintf("%s\n<referencedParameters>%s</referencedParameters>\n",
			cascadeStateXML, xmlEscape(opts.referenced))
	}

	return paramDef{
		xmlTag: tag,
		body: fmt.Sprintf(`<name>%s</name>
<description>%s</description>
<randomName>%s</randomName>
<visibleItemCount>%d</visibleItemCount>
%s
%s<choiceType>%s</choiceType>
<filterable>%t</filterable>
<filterLength>1</filterLength>`,
			xmlEscape(name),
			xmlEscape(description),
			xmlEscape("choice-parameter-"+name),
			opts.visibleItemCount,
			unoScriptXML(script, opts.fallback),
			cascade,
			opts.choiceType,
			opts.filterable,
		),
	}
}

// unoReference builds a DynamicReferenceParameter definition. Those render
// markup or an input widget instead of a select, and with omitValueField they
// submit no value at all.
func unoReference(name, description, script string, opts unoChoiceOpts) paramDef {
	opts = opts.withDefaults("ET_FORMATTED_HTML")

	return paramDef{
		xmlTag: "org.biouno.unochoice.DynamicReferenceParameter",
		body: fmt.Sprintf(`<name>%s</name>
<description>%s</description>
<randomName>%s</randomName>
<visibleItemCount>%d</visibleItemCount>
%s
%s
<referencedParameters>%s</referencedParameters>
<choiceType>%s</choiceType>
<omitValueField>%t</omitValueField>`,
			xmlEscape(name),
			xmlEscape(description),
			xmlEscape("choice-parameter-"+name),
			opts.visibleItemCount,
			unoScriptXML(script, opts.fallback),
			cascadeStateXML,
			xmlEscape(opts.referenced),
			opts.choiceType,
			opts.omitValueField,
		),
	}
}

// dynamicChoiceJobs returns the Active Choices fixture jobs, all under the
// "Dynamic" folder.
func dynamicChoiceJobs() []jobDef {
	parents := []string{"Dynamic"}

	return []jobDef{
		{
			name:    "cascade-infra-4-level",
			parents: parents,
			desc:    "Four-level cascade: cloud -> region -> cluster -> namespaces (multi-select)",
			config: multiParamPipelineXML("Four-level Active Choices cascade with a multi-select leaf",
				[]paramDef{
					unoChoice("CLOUD", "Cloud provider", "return ['aws', 'gcp', 'azure']", unoChoiceOpts{visibleItemCount: 3}),
					unoChoice("REGION", "Regions available in the selected cloud", `
def regions = [
    aws:   ['us-east-1', 'us-west-2', 'eu-west-1', 'eu-central-1'],
    gcp:   ['us-central1', 'europe-west4', 'asia-east1'],
    azure: ['eastus', 'westeurope'],
]
return regions.get(CLOUD, ['no-region'])`,
						unoChoiceOpts{referenced: "CLOUD", filterable: true, visibleItemCount: 4}),
					unoChoice("CLUSTER", "Clusters in the selected cloud and region (two parents)", `
if (!CLOUD || !REGION || REGION == 'no-region') {
    return []
}
return (1..3).collect { "${CLOUD}-${REGION}-k8s-0${it}" }`,
						unoChoiceOpts{referenced: "CLOUD,REGION", visibleItemCount: 3}),
					unoChoice("NAMESPACES", "Namespaces to roll out to (multi-select)", `
if (!CLUSTER) {
    return []
}
def namespaces = ['default', 'ingress', 'observability', 'payments']
if (CLUSTER.endsWith('-01')) {
    namespaces += 'canary'
}
return namespaces`,
						unoChoiceOpts{referenced: "CLUSTER", choiceType: "PT_MULTI_SELECT", visibleItemCount: 5}),
					stringParam("CHANGE_TICKET", "CHG-0000", "Change ticket reference"),
					boolParam("DRY_RUN", true, "Plan only, do not apply"),
				}, `
pipeline {
    agent any
    stages {
        stage('Resolve target') {
            steps {
                echo "Cloud:      ${params.CLOUD}"
                echo "Region:     ${params.REGION}"
                echo "Cluster:    ${params.CLUSTER}"
                echo "Namespaces: ${params.NAMESPACES}"
                echo "Ticket:     ${params.CHANGE_TICKET}"
                sh 'sleep 1'
            }
        }
        stage('Roll out') {
            steps {
                script {
                    def targets = (params.NAMESPACES ?: '').split(',').findAll { it?.trim() }
                    if (targets.isEmpty()) {
                        echo 'No namespace selected, nothing to do.'
                        return
                    }
                    for (ns in targets) {
                        echo "${params.DRY_RUN ? '[DRY RUN] would apply' : 'Applying'} ${params.CLUSTER}/${ns.trim()}"
                        sh 'sleep 1'
                    }
                }
            }
        }
    }
}`),
			builds: []map[string]string{
				{"CLOUD": "aws", "REGION": "eu-west-1", "CLUSTER": "aws-eu-west-1-k8s-01", "NAMESPACES": "default,canary", "CHANGE_TICKET": "CHG-1041", "DRY_RUN": "true"},
				{"CLOUD": "gcp", "REGION": "europe-west4", "CLUSTER": "gcp-europe-west4-k8s-02", "NAMESPACES": "ingress,observability,payments", "CHANGE_TICKET": "CHG-1042", "DRY_RUN": "false"},
			},
		},
		{
			name:    "multi-input-release-flow",
			parents: parents,
			desc:    "Cascading parameters that select between different pending-input gates",
			config: multiParamPipelineXML("Active Choices parameters driving different input flows",
				[]paramDef{
					unoChoice("RELEASE_TYPE", "Semver bump for this release", "return ['patch', 'minor', 'major']", unoChoiceOpts{visibleItemCount: 3}),
					unoChoice("TARGET_ENV", "Environments allowed for the selected release type", `
switch (RELEASE_TYPE) {
    case 'patch': return ['dev', 'staging']
    case 'minor': return ['staging']
    case 'major': return ['staging', 'production']
    default:      return ['dev']
}`, unoChoiceOpts{referenced: "RELEASE_TYPE", visibleItemCount: 3}),
					unoChoice("APPROVERS", "Approvers required for the selected environment (checkboxes)", `
if (TARGET_ENV == 'production') {
    return ['release-manager', 'sre-oncall', 'security']
}
if (TARGET_ENV == 'staging') {
    return ['release-manager', 'qa-lead']
}
return ['none-required']`,
						unoChoiceOpts{referenced: "TARGET_ENV", choiceType: "PT_CHECKBOX", visibleItemCount: 3}),
					unoChoice("ROLLBACK_BUILD", "Build to roll back to if the release fails", `
if (TARGET_ENV == 'dev') {
    return ['not-applicable']
}
return (1..5).collect { "release-1.4.${6 - it}" }`,
						unoChoiceOpts{referenced: "TARGET_ENV", filterable: true, visibleItemCount: 5}),
					boolParam("SKIP_TESTS", false, "Skip the verification stage"),
				}, `
pipeline {
    agent any
    stages {
        stage('Plan') {
            steps {
                echo "Release type: ${params.RELEASE_TYPE}"
                echo "Target env:   ${params.TARGET_ENV}"
                echo "Approvers:    ${params.APPROVERS}"
                echo "Rollback to:  ${params.ROLLBACK_BUILD}"
                sh 'sleep 1'
            }
        }
        stage('Verify') {
            when { expression { !params.SKIP_TESTS } }
            steps {
                echo 'Running release verification...'
                sh 'sleep 3'
                echo 'Verification passed.'
            }
        }
        stage('Gate: dev') {
            when { expression { params.TARGET_ENV == 'dev' } }
            steps {
                echo 'Dev releases are auto-approved, no input required.'
            }
        }
        stage('Gate: staging') {
            when { expression { params.TARGET_ENV == 'staging' } }
            steps {
                input message: "Promote ${params.RELEASE_TYPE} release to staging?", ok: 'Promote'
                echo 'Staging promotion approved.'
            }
        }
        stage('Gate: production') {
            when { expression { params.TARGET_ENV == 'production' } }
            steps {
                script {
                    def first = input(
                        message: 'Production release approval',
                        ok: 'Approve',
                        parameters: [
                            choice(name: 'WINDOW', choices: ['immediate', 'maintenance-window'], description: 'When to release'),
                            string(name: 'CHANGE_ID', defaultValue: 'CHG-0000', description: 'Change record'),
                            booleanParam(name: 'PAGE_ONCALL', defaultValue: true, description: 'Page on-call during rollout'),
                        ])
                    echo "Window: ${first.WINDOW}, change: ${first.CHANGE_ID}, page on-call: ${first.PAGE_ONCALL}"
                    input message: 'Second approver sign-off required for a major release', ok: 'Sign off'
                    echo 'Both production approvals recorded.'
                }
            }
        }
        stage('Release') {
            steps {
                echo "Releasing to ${params.TARGET_ENV}..."
                sh 'sleep 2'
                echo 'Release complete.'
            }
        }
    }
}`),
			builds: []map[string]string{
				// Auto-approved path: finishes without any input.
				{"RELEASE_TYPE": "patch", "TARGET_ENV": "dev", "APPROVERS": "none-required", "ROLLBACK_BUILD": "not-applicable", "SKIP_TESTS": "true"},
				// Single plain approval: parks on a pending input.
				{"RELEASE_TYPE": "minor", "TARGET_ENV": "staging", "APPROVERS": "release-manager,qa-lead", "ROLLBACK_BUILD": "release-1.4.5", "SKIP_TESTS": "false"},
				// Input with its own parameters, then a second approval.
				{"RELEASE_TYPE": "major", "TARGET_ENV": "production", "APPROVERS": "release-manager,sre-oncall,security", "ROLLBACK_BUILD": "release-1.4.4", "SKIP_TESTS": "true"},
			},
		},
		{
			name:    "filterable-service-matrix",
			parents: parents,
			desc:    "Long filterable option lists, radio buttons and checkbox regions",
			config: multiParamPipelineXML("Active Choices with large filterable lists and radio/checkbox types",
				[]paramDef{
					unoChoice("SERVICE", "Service to deploy (40 generated options, filterable)",
						"return (1..40).collect { String.format('svc-%02d-%s', it, it % 2 == 0 ? 'api' : 'worker') }",
						unoChoiceOpts{filterable: true, visibleItemCount: 10}),
					unoChoice("VERSION", "Versions published for the selected service", `
if (!SERVICE) {
    return []
}
def major = SERVICE.endsWith('api') ? 2 : 1
return (0..9).collect { "${major}.${9 - it}.0" } + ['latest', 'rollback-candidate']`,
						unoChoiceOpts{referenced: "SERVICE", filterable: true, visibleItemCount: 8}),
					unoChoice("STRATEGY", "Rollout strategy (radio)", "return ['rolling', 'blue-green', 'canary']",
						unoChoiceOpts{choiceType: "PT_RADIO", visibleItemCount: 3}),
					unoChoice("CANARY_PERCENT", "Canary share, only meaningful for the canary strategy", `
if (STRATEGY != 'canary') {
    return ['not-applicable']
}
return ['5', '10', '25', '50']`,
						unoChoiceOpts{referenced: "STRATEGY", visibleItemCount: 4}),
					unoChoice("REGIONS", "Regions to include (checkboxes)",
						"return ['us-east-1', 'us-west-2', 'eu-west-1', 'eu-central-1', 'ap-southeast-1']",
						unoChoiceOpts{choiceType: "PT_CHECKBOX", visibleItemCount: 5}),
					stringParam("REPLICAS", "3", "Replicas per region"),
				}, `
pipeline {
    agent any
    stages {
        stage('Plan') {
            steps {
                echo "Service:  ${params.SERVICE} @ ${params.VERSION}"
                echo "Strategy: ${params.STRATEGY} (canary ${params.CANARY_PERCENT}%)"
                echo "Regions:  ${params.REGIONS}"
                echo "Replicas: ${params.REPLICAS}"
                sh 'sleep 1'
            }
        }
        stage('Deploy regions') {
            steps {
                script {
                    def regions = (params.REGIONS ?: '').split(',').findAll { it?.trim() }
                    if (regions.isEmpty()) {
                        error 'No region selected.'
                    }
                    for (region in regions) {
                        echo "Deploying ${params.SERVICE}:${params.VERSION} to ${region.trim()} (${params.STRATEGY})"
                        sh 'sleep 1'
                    }
                }
            }
        }
    }
}`),
			builds: []map[string]string{
				{"SERVICE": "svc-04-api", "VERSION": "2.7.0", "STRATEGY": "canary", "CANARY_PERCENT": "10", "REGIONS": "us-east-1,eu-west-1", "REPLICAS": "4"},
				{"SERVICE": "svc-13-worker", "VERSION": "latest", "STRATEGY": "rolling", "CANARY_PERCENT": "not-applicable", "REGIONS": "us-west-2", "REPLICAS": "2"},
			},
		},
		{
			name:    "env-fleet-picker",
			parents: parents,
			desc:    "Oversized option lists (50-60 entries) for exercising picker search",
			config: multiParamPipelineXML("Active Choices lists too long to scan by eye",
				[]paramDef{
					// 50 options, deliberately NOT filterable. The plugin's own
					// filter box is absent, so this is the case that proves the
					// TUI picker's search stands on its own: opening on the list
					// and reaching dev47 must be possible with "/" alone.
					unoChoice("ENVIRONMENT", "Ephemeral environment (50 options, no plugin filter)",
						"return (1..50).collect { String.format('dev%02d', it) }",
						unoChoiceOpts{visibleItemCount: 10}),
					// Same size, filterable: true. The picker should open with
					// its filter already focused here and not above, which is
					// the only behavioural difference the flag drives.
					unoChoice("DATACENTER", "Datacenter (60 options, plugin marks it filterable)",
						"return (1..60).collect { String.format('dc-%s-%02d', ['ams', 'fra', 'iad', 'sjc'][it % 4], it) }",
						unoChoiceOpts{filterable: true, visibleItemCount: 10}),
					// Multi-select AND long AND cascading: the combination that
					// breaks naive pickers, because selections are keyed by list
					// index and the filter renumbers the list under them. Select
					// two, filter, select a third, clear the filter -> all three
					// must survive.
					unoChoice("SERVICES", "Services to roll out (multi-select, 60 options, cascades on ENVIRONMENT)", `
if (!ENVIRONMENT || !ENVIRONMENT.startsWith('dev')) {
    return []
}
def suffix = ENVIRONMENT.substring(3)
return (1..60).collect { String.format('%s-svc-%02d', suffix, it) }`,
						unoChoiceOpts{
							referenced:       "ENVIRONMENT",
							choiceType:       "PT_MULTI_SELECT",
							visibleItemCount: 10,
						}),
					stringParam("TICKET", "OPS-0000", "Change ticket"),
				}, `
pipeline {
    agent any
    stages {
        stage('Plan') {
            steps {
                echo "Environment: ${params.ENVIRONMENT}"
                echo "Datacenter:  ${params.DATACENTER}"
                echo "Ticket:      ${params.TICKET}"
            }
        }
        stage('Roll out') {
            steps {
                script {
                    def services = (params.SERVICES ?: '').split(',').findAll { it?.trim() }
                    if (services.isEmpty()) {
                        error 'No service selected.'
                    }
                    echo "Rolling out ${services.size()} service(s) to ${params.ENVIRONMENT}"
                    for (svc in services) {
                        echo "  ${svc.trim()} -> ${params.DATACENTER}"
                    }
                    sh 'sleep 1'
                }
            }
        }
    }
}`),
			builds: []map[string]string{
				{"ENVIRONMENT": "dev07", "DATACENTER": "dc-sjc-03", "SERVICES": "07-svc-01,07-svc-14", "TICKET": "OPS-4711"},
				{"ENVIRONMENT": "dev42", "DATACENTER": "dc-ams-60", "SERVICES": "42-svc-03,42-svc-28,42-svc-59", "TICKET": "OPS-4712"},
			},
		},
		{
			name:    "dynamic-reference-summary",
			parents: parents,
			desc:    "DynamicReferenceParameter render types (HTML, text box, list) next to cascading choices",
			config: multiParamPipelineXML("Active Choices reference parameters rendered from other parameters",
				[]paramDef{
					unoChoice("ENV", "Target environment", "return ['dev', 'staging', 'production']", unoChoiceOpts{visibleItemCount: 3}),
					unoChoice("FEATURES", "Feature flags to enable (checkboxes)", `
def flags = ['new-checkout', 'search-v2', 'dark-mode']
if (ENV == 'production') {
    flags = flags - 'search-v2'
}
return flags`,
						unoChoiceOpts{referenced: "ENV", choiceType: "PT_CHECKBOX", visibleItemCount: 3}),
					// Markup only: submits no value at all.
					unoReference("PLAN_SUMMARY", "Rendered plan summary (no submitted value)", `
def flags = (FEATURES ?: '').split(',').findAll { it?.trim() }
def rows = flags.collect { "<li>${it.trim()}</li>" }.join('')
return """<b>Environment:</b> ${ENV ?: 'none'}<br/>
<b>Flags (${flags.size()}):</b><ul>${rows ?: '<li>none</li>'}</ul>"""`,
						unoChoiceOpts{referenced: "ENV,FEATURES", choiceType: "ET_FORMATTED_HTML", omitValueField: true}),
					// Text box pre-filled from other parameters: submits a value.
					unoReference("OPERATOR_NOTE", "Editable note, pre-filled from the selection",
						`return "Enabling ${(FEATURES ?: 'nothing')} on ${(ENV ?: 'unknown')}"`,
						unoChoiceOpts{referenced: "ENV,FEATURES", choiceType: "ET_TEXT_BOX"}),
					unoReference("CHECKLIST", "Pre-flight checklist for the environment", `
def items = ['Backup verified', 'Dashboards open']
if (ENV == 'production') {
    items += ['Change record approved', 'On-call paged']
}
return items`,
						unoChoiceOpts{referenced: "ENV", choiceType: "ET_ORDERED_LIST", omitValueField: true}),
					boolParam("NOTIFY", true, "Post the summary to chat"),
				}, `
pipeline {
    agent any
    stages {
        stage('Summary') {
            steps {
                echo "Env:      ${params.ENV}"
                echo "Features: ${params.FEATURES}"
                echo "Note:     ${params.OPERATOR_NOTE}"
                sh 'sleep 1'
            }
        }
        stage('Apply flags') {
            steps {
                script {
                    def flags = (params.FEATURES ?: '').split(',').findAll { it?.trim() }
                    for (flag in flags) {
                        echo "Enabling ${flag.trim()} on ${params.ENV}"
                        sh 'sleep 1'
                    }
                }
            }
        }
        stage('Notify') {
            when { expression { params.NOTIFY } }
            steps {
                echo 'Summary posted.'
            }
        }
    }
}`),
			builds: []map[string]string{
				{"ENV": "staging", "FEATURES": "new-checkout,search-v2", "OPERATOR_NOTE": "Enabling new-checkout,search-v2 on staging", "NOTIFY": "true"},
				{"ENV": "production", "FEATURES": "dark-mode", "OPERATOR_NOTE": "Flag rollout for the Friday window", "NOTIFY": "false"},
			},
		},
		{
			name:    "choice-script-edge-cases",
			parents: parents,
			desc:    "Failing, slow, empty and oversized Active Choices scripts",
			config: multiParamPipelineXML("Active Choices error, latency and size edge cases",
				[]paramDef{
					// Main script always throws, so the fallback list is served.
					unoChoice("PRIMARY_REGION", "Script throws, options come from the fallback script",
						"throw new RuntimeException('choice backend unavailable')",
						unoChoiceOpts{
							fallback:         "return ['fallback-region-a', 'fallback-region-b']",
							visibleItemCount: 2,
						}),
					// CPU-bound instead of Thread.sleep, which the Groovy sandbox rejects.
					// Sandboxed Groovy intercepts every operation, so this loop costs
					// roughly a second — enough for a loading state, small enough that it
					// does not stall the job API for the rest of the TUI.
					unoChoice("SLOW_INVENTORY", "Slow script, useful for loading and cancellation states", `
long acc = 0
for (int i = 0; i < 200000; i++) {
    acc += i
}
return ['inventory-a', 'inventory-b', "checksum-${acc % 97}"]`,
						unoChoiceOpts{referenced: "PRIMARY_REGION", visibleItemCount: 3}),
					// Empty result once a parent is selected.
					unoChoice("EMPTY_SET", "Always resolves to an empty option list", "return []",
						unoChoiceOpts{referenced: "PRIMARY_REGION"}),
					// 500 options, no filter: exercises list bounds and rendering cost.
					unoChoice("LARGE_LIST", "500 options without a filter",
						"return (1..500).collect { String.format('node-%03d.internal', it) }",
						unoChoiceOpts{visibleItemCount: 10}),
					unoChoice("ODD_LABELS", "Options with characters that need escaping", `
return ['plain', 'with space', 'with,comma', 'with"quote', "with'apostrophe", 'with<angle>', 'with&amp', 'ünïcodé']`,
						unoChoiceOpts{visibleItemCount: 8}),
					unoChoice("LABELLED_TARGET", "Map choices distinguish submitted values from labels", `
return ['prod-eu':'EU Production:selected', 'prod-us':'US Production:disabled', 'staging':'Staging:disabled:selected']`,
						unoChoiceOpts{referenced: "PRIMARY_REGION", visibleItemCount: 3}),
					unoChoice("COMMA_VALUES", "Multi-select values include the plugin's ambiguous comma case", `
return ['plain:selected', 'with,comma:selected', 'unavailable:disabled:selected']`,
						unoChoiceOpts{choiceType: "PT_MULTI_SELECT", visibleItemCount: 3}),
					stringParam("REASON", "edge-case sweep", "Free-form reason"),
				}, `
pipeline {
    agent any
    stages {
        stage('Report') {
            steps {
                echo "Primary region: ${params.PRIMARY_REGION}"
                echo "Inventory:      ${params.SLOW_INVENTORY}"
                echo "Empty set:      '${params.EMPTY_SET}'"
                echo "Large list:     ${params.LARGE_LIST}"
                echo "Odd labels:     ${params.ODD_LABELS}"
                echo "Reason:         ${params.REASON}"
                sh 'sleep 2'
            }
        }
    }
}`),
			builds: []map[string]string{
				{"PRIMARY_REGION": "fallback-region-a", "SLOW_INVENTORY": "inventory-b", "EMPTY_SET": "", "LARGE_LIST": "node-042.internal", "ODD_LABELS": "with,comma", "REASON": "edge-case sweep"},
			},
		},
	}
}
