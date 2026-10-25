package logviewer

import "testing"

func TestBufferWindow_SetTotalLines(t *testing.T) {
	tests := []struct {
		name             string
		totalLines       int
		visibleLineCount int
		wantFrom         int
		wantTo           int
	}{
		{
			name:             "small file fits in buffer",
			totalLines:       50,
			visibleLineCount: 30,
			wantFrom:         0,
			wantTo:           50, // min(250+30, 50) = 50
		},
		{
			name:             "large file clamps to init hidden + visible",
			totalLines:       10000,
			visibleLineCount: 40,
			wantFrom:         0,
			wantTo:           InitHiddenLines + 40, // 290
		},
		{
			name:             "exact boundary",
			totalLines:       InitHiddenLines + 50,
			visibleLineCount: 50,
			wantFrom:         0,
			wantTo:           InitHiddenLines + 50,
		},
		{
			name:             "zero lines",
			totalLines:       0,
			visibleLineCount: 40,
			wantFrom:         0,
			wantTo:           0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bw := &BufferWindow{}
			bw.SetTotalLines(tt.totalLines, tt.visibleLineCount)

			if bw.TotalLines != tt.totalLines {
				t.Errorf("TotalLines = %d, want %d", bw.TotalLines, tt.totalLines)
			}

			from, to := bw.GetRange()
			if from != tt.wantFrom {
				t.Errorf("From = %d, want %d", from, tt.wantFrom)
			}
			if to != tt.wantTo {
				t.Errorf("To = %d, want %d", to, tt.wantTo)
			}
		})
	}
}

func TestBufferWindow_IsFirstChunk(t *testing.T) {
	tests := []struct {
		name string
		from int
		want bool
	}{
		{"at start", 0, true},
		{"not at start", 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bw := &BufferWindow{From: tt.from, To: 500, TotalLines: 1000}
			if got := bw.IsFirstChunk(); got != tt.want {
				t.Errorf("IsFirstChunk() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBufferWindow_IsLastChunk(t *testing.T) {
	tests := []struct {
		name string
		to   int
		want bool
	}{
		{"at end", 1000, true},
		{"past end", 1500, true},
		{"not at end", 500, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bw := &BufferWindow{From: 0, To: tt.to, TotalLines: 1000}
			if got := bw.IsLastChunk(); got != tt.want {
				t.Errorf("IsLastChunk() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBufferWindow_ShouldLoadDownFor(t *testing.T) {
	tests := []struct {
		name string
		bw   BufferWindow
		line int
		want bool
	}{
		{
			name: "plenty of hidden lines below",
			bw:   BufferWindow{From: 0, To: 500, TotalLines: 10000},
			line: 300,
			want: false, // 500 - 300 = 200 > MinimumHiddenLines(50)
		},
		{
			name: "near bottom of buffer triggers load",
			bw:   BufferWindow{From: 0, To: 500, TotalLines: 10000},
			line: 460,
			want: true, // 500 - 460 = 40 <= 50
		},
		{
			name: "exactly at threshold",
			bw:   BufferWindow{From: 0, To: 500, TotalLines: 10000},
			line: 450,
			want: true, // 500 - 450 = 50 <= 50
		},
		{
			name: "at end of file no load needed",
			bw:   BufferWindow{From: 0, To: 1000, TotalLines: 1000},
			line: 990,
			want: false, // To >= TotalLines
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.bw.ShouldLoadDownFor(tt.line); got != tt.want {
				t.Errorf("ShouldLoadDownFor(%d) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestBufferWindow_ShouldLoadUpFor(t *testing.T) {
	tests := []struct {
		name string
		bw   BufferWindow
		line int
		want bool
	}{
		{
			name: "plenty of hidden lines above",
			bw:   BufferWindow{From: 100, To: 500, TotalLines: 10000},
			line: 300,
			want: false, // 300 - 100 = 200 >= 50
		},
		{
			name: "near top of buffer triggers load",
			bw:   BufferWindow{From: 100, To: 500, TotalLines: 10000},
			line: 140,
			want: true, // 140 - 100 = 40 < 50
		},
		{
			name: "at start of file no load needed",
			bw:   BufferWindow{From: 0, To: 500, TotalLines: 10000},
			line: 10,
			want: false, // From == 0
		},
		{
			name: "exactly at threshold",
			bw:   BufferWindow{From: 100, To: 500, TotalLines: 10000},
			line: 149,
			want: true, // 149 - 100 = 49 < 50
		},
		{
			name: "one past threshold",
			bw:   BufferWindow{From: 100, To: 500, TotalLines: 10000},
			line: 150,
			want: false, // 150 - 100 = 50, not < 50
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.bw.ShouldLoadUpFor(tt.line); got != tt.want {
				t.Errorf("ShouldLoadUpFor(%d) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestBufferWindow_CalculateNewBufferRangeDown(t *testing.T) {
	tests := []struct {
		name             string
		initial          BufferWindow
		targetLine       int
		visibleLineCount int
		wantFrom         int
		wantTo           int
	}{
		{
			name:             "target in middle of file",
			initial:          BufferWindow{From: 0, To: 500, TotalLines: 10000},
			targetLine:       600,
			visibleLineCount: 50,
			wantFrom:         450, // max(0, 600-50-100)
			wantTo:           700, // min(600+100, 10000)
		},
		{
			name:             "target near end of file clamps To",
			initial:          BufferWindow{From: 0, To: 500, TotalLines: 650},
			targetLine:       600,
			visibleLineCount: 50,
			wantFrom:         450,
			wantTo:           650, // clamped to TotalLines
		},
		{
			name:             "target near start clamps From to 0",
			initial:          BufferWindow{From: 0, To: 500, TotalLines: 10000},
			targetLine:       100,
			visibleLineCount: 50,
			wantFrom:         0, // max(0, 100-50-100) = max(0, -50) = 0
			wantTo:           200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bw := tt.initial
			bw.CalculateNewBufferRangeDown(tt.targetLine, tt.visibleLineCount)

			from, to := bw.GetRange()
			if from != tt.wantFrom {
				t.Errorf("From = %d, want %d", from, tt.wantFrom)
			}
			if to != tt.wantTo {
				t.Errorf("To = %d, want %d", to, tt.wantTo)
			}
		})
	}
}

func TestBufferWindow_CalculateNewBufferRangeUp(t *testing.T) {
	tests := []struct {
		name             string
		initial          BufferWindow
		targetLine       int
		visibleLineCount int
		wantFrom         int
		wantTo           int
	}{
		{
			name:             "target in middle of file",
			initial:          BufferWindow{From: 500, To: 1000, TotalLines: 10000},
			targetLine:       400,
			visibleLineCount: 50,
			wantFrom:         300, // max(0, 400-100)
			wantTo:           550, // min(10000, 400+50+100)
		},
		{
			name:             "target near start clamps From to 0",
			initial:          BufferWindow{From: 500, To: 1000, TotalLines: 10000},
			targetLine:       50,
			visibleLineCount: 50,
			wantFrom:         0, // max(0, 50-100) = 0
			wantTo:           200,
		},
		{
			name:             "target near end clamps To",
			initial:          BufferWindow{From: 500, To: 1000, TotalLines: 550},
			targetLine:       480,
			visibleLineCount: 50,
			wantFrom:         380,
			wantTo:           550, // clamped to TotalLines
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bw := tt.initial
			bw.CalculateNewBufferRangeUp(tt.targetLine, tt.visibleLineCount)

			from, to := bw.GetRange()
			if from != tt.wantFrom {
				t.Errorf("From = %d, want %d", from, tt.wantFrom)
			}
			if to != tt.wantTo {
				t.Errorf("To = %d, want %d", to, tt.wantTo)
			}
		})
	}
}

func TestBufferWindow_ShouldLoadDown(t *testing.T) {
	bw := BufferWindow{From: 0, To: 500, TotalLines: 10000}

	if got := bw.ShouldLoadDown(300); got {
		t.Error("ShouldLoadDown(300) should be false, plenty of hidden lines")
	}
	if got := bw.ShouldLoadDown(460); !got {
		t.Error("ShouldLoadDown(460) should be true, near buffer edge")
	}
}

func TestBufferWindow_ShouldLoadUp(t *testing.T) {
	// ShouldLoadUp checks ShouldLoadUpFor(MinimumHiddenLines)
	bw := BufferWindow{From: 0, To: 500, TotalLines: 10000}
	if got := bw.ShouldLoadUp(); got {
		t.Error("ShouldLoadUp() should be false when From==0")
	}

	bw2 := BufferWindow{From: 100, To: 500, TotalLines: 10000}
	// ShouldLoadUpFor(50): 50 - 100 = -50 < 50 → true
	if got := bw2.ShouldLoadUp(); !got {
		t.Error("ShouldLoadUp() should be true when MinimumHiddenLines - From < MinimumHiddenLines")
	}
}
