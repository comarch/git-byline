package report

import (
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
)

func TestCollectMergesSessionUsage(t *testing.T) {
	t.Parallel()
	root := reportTestRepository(t)
	writeReportFile(t, root, "file", "one\ntwo\n")
	head := reportCommit(t, root, "usage")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	writeReportNote(t, repo, head, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file": {Blob: "badcafe1", Ranges: []model.Range{{
				Start: 1, End: 2,
				Attribution: model.Attribution{
					Author: model.AuthorAI, Agent: "droid", Model: "model-a", Session: "session-a",
				},
			}}},
		},
		Sessions: map[string]model.NoteSession{
			model.NoteSessionKey("droid", "session-a"): {
				Agent: "droid", Model: "model-a",
				Added: 2, Accepted: 2,
				TokensIn:   120,
				TokensOut:  34,
				CacheRead:  56,
				CacheWrite: 78,
			},
		},
	})

	got, err := Collect(repo, "", head, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("sessions = %+v, want one", got.Sessions)
	}
	session := got.Sessions[0]
	if session.Session != "session-a" || session.Agent != "droid" || session.Model != "model-a" {
		t.Fatalf("session identity = %+v", session)
	}
	if session.Lines != 2 {
		t.Fatalf("session lines = %d, want 2", session.Lines)
	}
	if session.TokensIn != 120 || session.TokensOut != 34 ||
		session.CacheRead != 56 || session.CacheWrite != 78 {
		t.Fatalf("session usage = %+v, want 120/34/56/78", session)
	}
}

func TestCollectKeepsUsageForSessionWithoutLines(t *testing.T) {
	t.Parallel()
	root := reportTestRepository(t)
	writeReportFile(t, root, "file", "one\n")
	head := reportCommit(t, root, "usage only")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	writeReportNote(t, repo, head, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file": {Blob: "badcafe1", Ranges: []model.Range{{
				Start: 1, End: 1,
				Attribution: model.Attribution{Author: model.AuthorHuman},
			}}},
		},
		Sessions: map[string]model.NoteSession{
			model.NoteSessionKey("droid", "session-a"): {
				Agent:    "droid",
				TokensIn: 120, TokensOut: 34,
			},
		},
	})

	got, err := Collect(repo, "", head, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("sessions = %+v, want one", got.Sessions)
	}
	session := got.Sessions[0]
	if session.Session != "session-a" || session.Lines != 0 || session.Model != "unknown" {
		t.Fatalf("session identity = %+v", session)
	}
	if session.TokensIn != 120 || session.TokensOut != 34 {
		t.Fatalf("session usage = %+v, want 120/34", session)
	}
}

func TestCollectSumsSessionUsageAcrossCommits(t *testing.T) {
	t.Parallel()
	root := reportTestRepository(t)
	writeReportFile(t, root, "file", "one\n")
	first := reportCommit(t, root, "first")
	writeReportFile(t, root, "file", "one\ntwo\n")
	second := reportCommit(t, root, "second")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	session := model.NoteSession{
		Agent: "droid", Model: "model-a",
		Added: 1, Accepted: 1,
		TokensIn: 10, TokensOut: 4, CacheRead: 2, CacheWrite: 1,
	}
	writeReportNote(t, repo, first, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file": {Blob: "badcafe1", Ranges: []model.Range{{
				Start: 1, End: 1,
				Attribution: model.Attribution{
					Author: model.AuthorAI, Agent: "droid", Model: "model-a", Session: "session-a",
				},
			}}},
		},
		Sessions: map[string]model.NoteSession{
			model.NoteSessionKey("droid", "session-a"): session,
		},
	})
	writeReportNote(t, repo, second, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file": {Blob: "faceb00c", Ranges: []model.Range{{
				Start: 1, End: 2,
				Attribution: model.Attribution{
					Author: model.AuthorAI, Agent: "droid", Model: "model-a", Session: "session-a",
				},
			}}},
		},
		Sessions: map[string]model.NoteSession{
			model.NoteSessionKey("droid", "session-a"): session,
		},
	})

	got, err := Collect(repo, "", second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("sessions = %+v, want one", got.Sessions)
	}
	value := got.Sessions[0]
	if value.TokensIn != 20 || value.TokensOut != 8 ||
		value.CacheRead != 4 || value.CacheWrite != 2 {
		t.Fatalf("session usage = %+v, want 20/8/4/2", value)
	}
}

func TestMergeSessionUsageSkipsEmptyUsage(t *testing.T) {
	t.Parallel()
	sessions := map[string]*SessionTotals{}
	mergeSessionUsage(sessions, map[string]model.NoteSession{
		model.NoteSessionKey("droid", "session-a"): {Agent: "droid"},
	})
	if len(sessions) != 0 {
		t.Fatalf("sessions = %+v, want none", sessions)
	}
}
