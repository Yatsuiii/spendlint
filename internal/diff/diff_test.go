package diff

import "testing"

const goldPathDiff = `--- a/summarize.py
+++ b/summarize.py
@@ -12,7 +12,7 @@
 def summarize(text):
-    # spendlint:label summary_endpoint
-    response = client.messages.create(model="claude-3-haiku", max_tokens=512, ...)
+    # spendlint:label summary_endpoint
+    response = client.messages.create(model="claude-3-5-sonnet", max_tokens=512, ...)
     return response.content
`

func TestClassifyModelSwap(t *testing.T) {
	hunks, err := Parse(goldPathDiff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	changes := ClassifyAll(hunks)
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}
	c := changes[0]
	if c.Type != ChangeModelSwap {
		t.Errorf("type = %v, want model_swap", c.Type)
	}
	if c.OldValue != "claude-3-haiku" {
		t.Errorf("old = %q, want claude-3-haiku", c.OldValue)
	}
	if c.NewValue != "claude-3-5-sonnet" {
		t.Errorf("new = %q, want claude-3-5-sonnet", c.NewValue)
	}
	if c.Label != "summary_endpoint" {
		t.Errorf("label = %q, want summary_endpoint", c.Label)
	}
}

const maxTokensDiff = `--- a/chat.go
+++ b/chat.go
@@ -5,5 +5,5 @@
 // spendlint:label chat_assistant
-    MaxTokens: 512,
+    MaxTokens: 4096,
`

func TestClassifyMaxTokens(t *testing.T) {
	hunks, err := Parse(maxTokensDiff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	changes := ClassifyAll(hunks)
	if len(changes) != 1 || changes[0].Type != ChangeMaxTokens {
		t.Fatalf("got %v, want max_tokens", changes)
	}
	if changes[0].OldValue != "512" || changes[0].NewValue != "4096" {
		t.Errorf("old/new = %s/%s", changes[0].OldValue, changes[0].NewValue)
	}
}

const retryDiff = `--- a/classify.py
+++ b/classify.py
@@ -3,3 +3,5 @@
 # spendlint:label classify_ticket
+for attempt in range(3):
     result = client.chat.completions.create(model="gpt-4o-mini", ...)
+    if result: break
`

func TestClassifyVolumeAdded(t *testing.T) {
	hunks, err := Parse(retryDiff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	changes := ClassifyAll(hunks)
	if len(changes) == 0 {
		t.Fatal("no changes classified")
	}
	found := false
	for _, c := range changes {
		if c.Type == ChangeVolumeAdded {
			found = true
		}
	}
	if !found {
		t.Errorf("expected volume_added, got %v", changes)
	}
}
