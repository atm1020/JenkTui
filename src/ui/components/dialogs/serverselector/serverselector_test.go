package serverselector

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
)

func TestManagementActionsCarryOnlyIdentity(t *testing.T) {
	conf := config.GetDefaultConfig().KeyMapConfig.Dialog.ServerSelector
	conf.Add = context.KeyBinding{"n"}
	conf.Edit = context.KeyBinding{"r"}
	d := New([]context.ServerConfig{{ID: "stable", Name: "Local", APIToken: "not-for-form"}}, 100, 30, conf)
	for _, tc := range []struct {
		code rune
		id   string
	}{{'n', ""}, {'r', "stable"}} {
		_, cmd := d.Update(tea.KeyPressMsg{Code: tc.code})
		if cmd == nil {
			t.Fatal("configured management key not handled")
		}
		msg, ok := cmd().(SetupMsg)
		if !ok || msg.ID != tc.id {
			t.Fatal("management action did not carry expected identity")
		}
	}
}
