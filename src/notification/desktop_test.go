package notification

import "testing"

type recordingDriver struct {
	notifyCalls int
	alertCalls  int
	beepCalls   int
}

func (d *recordingDriver) Notify(string, string) error {
	d.notifyCalls++
	return nil
}

func (d *recordingDriver) Alert(string, string) error {
	d.alertCalls++
	return nil
}

func (d *recordingDriver) Beep(float64, int) error {
	d.beepCalls++
	return nil
}

// beeep.Alert marks Linux notifications as critical, which many i3
// notification daemons render with a red background. Sound must therefore be
// added separately to a normal-urgency notification.
func TestDesktopSendWithSoundUsesNormalNotificationAndSeparateBeep(t *testing.T) {
	driver := &recordingDriver{}
	desktop := newDesktop(driver)

	if err := desktop.Send("demo #42 — Success", "demo", Options{Sound: true}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if driver.notifyCalls != 1 {
		t.Errorf("Notify() calls = %d, want 1", driver.notifyCalls)
	}
	if driver.beepCalls != 1 {
		t.Errorf("Beep() calls = %d, want 1", driver.beepCalls)
	}
	if driver.alertCalls != 0 {
		t.Errorf("Alert() calls = %d, want 0", driver.alertCalls)
	}
}

func TestDesktopSendAlertWithoutSoundStaysNormalAndSilent(t *testing.T) {
	driver := &recordingDriver{}
	desktop := newDesktop(driver)

	if err := desktop.Send("demo #42 — Failed", "demo", Options{Alert: true}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if driver.notifyCalls != 1 {
		t.Errorf("Notify() calls = %d, want 1", driver.notifyCalls)
	}
	if driver.alertCalls != 0 {
		t.Errorf("Alert() calls = %d, want 0", driver.alertCalls)
	}
	if driver.beepCalls != 0 {
		t.Errorf("Beep() calls = %d, want 0", driver.beepCalls)
	}
}

func TestDesktopSendFailedBuildWithSoundUsesAlert(t *testing.T) {
	driver := &recordingDriver{}
	desktop := newDesktop(driver)

	if err := desktop.Send("demo #42 — Failed", "demo", Options{Sound: true, Alert: true}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if driver.alertCalls != 1 {
		t.Errorf("Alert() calls = %d, want 1", driver.alertCalls)
	}
	if driver.notifyCalls != 0 {
		t.Errorf("Notify() calls = %d, want 0", driver.notifyCalls)
	}
	if driver.beepCalls != 0 {
		t.Errorf("Beep() calls = %d, want 0", driver.beepCalls)
	}
}
