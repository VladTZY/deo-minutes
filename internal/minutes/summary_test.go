package minutes

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"deo-minutes/internal/llm"
)

func TestSummarizeSinglePass(t *testing.T) {
	var got []llm.Request
	x := &Extractor{LLM: llm.Func(func(_ context.Context, req llm.Request) ([]byte, error) {
		got = append(got, req)
		return json.Marshal(Summary{Overview: "Bugetul a fost aprobat.", Decisions: []string{"Se aprobă bugetul."}})
	})}
	s, err := x.Summarize(context.Background(), MeetingInfo{Project: "Alfa", Date: time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)}, sample)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || s.Overview != "Bugetul a fost aprobat." || len(s.Decisions) != 1 {
		t.Fatalf("calls=%d summary=%+v", len(got), s)
	}
	req := got[0]
	if req.System != summaryPrompt || !strings.Contains(req.Context, "<project>Alfa</project>") ||
		!strings.Contains(req.Context, sample[0].Text) || req.Schema["additionalProperties"] != false {
		t.Errorf("request: %+v", req)
	}
}

func TestSummarizeWindowedMerge(t *testing.T) {
	var mu sync.Mutex
	var tasks []string
	x := &Extractor{SinglePassTokens: 1, Window: 150 * time.Second, Overlap: 10 * time.Second,
		LLM: llm.Func(func(_ context.Context, req llm.Request) ([]byte, error) {
			mu.Lock()
			tasks = append(tasks, req.User)
			mu.Unlock()
			return json.Marshal(Summary{Overview: "parte"})
		})}
	if _, err := x.Summarize(context.Background(), MeetingInfo{Date: time.Now()}, sample); err != nil {
		t.Fatal(err)
	}
	// 2 windows + 1 merge, the merge last and without the transcript
	if len(tasks) != 3 || !strings.HasPrefix(tasks[2], "These are the summaries") {
		t.Fatalf("tasks: %q", tasks)
	}
}

func TestSummarizeEmptyTranscript(t *testing.T) {
	x := &Extractor{LLM: llm.Func(func(context.Context, llm.Request) ([]byte, error) { t.Fatal("must not call"); return nil, nil })}
	if _, err := x.Summarize(context.Background(), MeetingInfo{}, nil); err == nil {
		t.Fatal("want error")
	}
}
