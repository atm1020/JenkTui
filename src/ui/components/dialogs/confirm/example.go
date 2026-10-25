package confirm

// Example usage of the confirmation dialog component
//
// Import path: "github.com/atm1020/jenktui/src/ui/components/dialogs/confirm"
//
// The confirm dialog is a reusable component for showing "Are you sure?" style
// confirmations to the user with Yes/No buttons.
//
// Usage:
//
// 1. Create a command to execute on confirmation:
//    onConfirm := func() tea.Msg {
//        // Perform the action (e.g., delete job, trigger build, etc.)
//        return MyActionCompletedMsg{}
//    }
//
// 2. (Optional) Create a command to execute on cancellation:
//    onCancel := func() tea.Msg {
//        return util.FooterNotiMsg{Message: "Action cancelled"}
//    }
//
// 3. Create the dialog:
//    dialog := confirm.NewConfirmDialog(
//        "Are you sure you want to delete this job?",
//        onConfirm,
//        onCancel, // can be nil if no action needed
//    )
//
// 4. Send the OpenDialogMsg to open it:
//    return util.CmdHandler(util.OpenDialogMsg{Dialog: dialog})
//
// Full example in a component's Update method:
//
//    func (m *MyComponent) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
//        switch msg := msg.(type) {
//        case tea.KeyPressMsg:
//            switch {
//            case key.Matches(msg, m.keymap.Delete):
//                // User pressed delete key, show confirmation
//                onConfirm := func() tea.Msg {
//                    // Perform the actual deletion
//                    return deleteJobMsg{jobID: m.selectedJob}
//                }
//                onCancel := func() tea.Msg {
//                    return util.FooterNotiMsg{Message: "Deletion cancelled"}
//                }
//                dialog := confirm.NewConfirmDialog(
//                    "Are you sure you want to delete this job?",
//                    onConfirm,
//                    onCancel,
//                )
//                return m, util.CmdHandler(util.OpenDialogMsg{Dialog: dialog})
//            }
//        case deleteJobMsg:
//            // Actually delete the job after confirmation
//            return m, m.performDeletion(msg.jobID)
//        }
//        return m, nil
//    }
//
// Keyboard Controls:
// - Left/Right arrows or Tab: Switch between Yes and No
// - Y: Direct "Yes"
// - N or ESC: Direct "No" or cancel
// - Enter/Space: Confirm the selected option
//
// The dialog defaults to "No" for safety, requiring explicit user action
// to confirm destructive operations.
