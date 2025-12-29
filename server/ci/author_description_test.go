package ci

import (
	"strings"
	"testing"
)

func TestEvent_AuthorAndDescriptionInTemplates(t *testing.T) {
	configYAML := `
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - webhook:
      url: https://example.com/webhook
      method: POST
      body: |
        {
          "rev": "{{ .Rev }}",
          "author": "{{ .Author }}",
          "description": "{{ .Description }}"
        }
`

	event := Event{
		Rev:         "main",
		ArchiveUrl:  "https://example.com/archive",
		Author:      "johndoe",
		Description: "Add new CI feature",
	}

	config, _, err := UnmarshalConfig([]byte(configYAML), event)
	if err != nil {
		t.Fatalf("UnmarshalConfig() error = %v", err)
	}

	if config.Do[0].Webhook == nil {
		t.Fatal("Expected webhook task but got nil")
	}

	body := config.Do[0].Webhook.Body
	if !strings.Contains(body, `"author": "johndoe"`) {
		t.Errorf("Expected body to contain author 'johndoe', got: %s", body)
	}
	if !strings.Contains(body, `"description": "Add new CI feature"`) {
		t.Errorf("Expected body to contain description 'Add new CI feature', got: %s", body)
	}
}
