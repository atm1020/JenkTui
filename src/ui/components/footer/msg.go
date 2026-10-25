package footer

import "time"

type UpdateFooterUrlMsg struct {
	Url string
}

type FooterProcessMsg struct {
	Message string
}

type FooterCleanProcessMsg time.Time
