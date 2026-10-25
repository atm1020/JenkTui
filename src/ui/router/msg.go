package router

import (
	"github.com/atm1020/jenktui/src/ui/util"
)

// NavigateMsg triggers navigation to a specific view
type NavigateMsg struct {
	Model util.JenkTuiModel
}

// BackMsg triggers navigation back to previous view
type BackMsg struct{}

type ForwardMsg struct{}

type JobSelectedNavigateMsg struct{}
