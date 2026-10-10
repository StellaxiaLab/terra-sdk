package svi

import "testing"

func TestParseSchemaRef(t *testing.T) {
	t.Parallel()

	ref, err := ParseSchemaRef("terra.video.frame@1")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Name != "video.frame" || ref.Major != 1 {
		t.Fatalf("unexpected schema ref: %+v", ref)
	}
}

func TestParseSchemaRefRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"video.frame@1",
		"terra.Video.Frame@1",
		"terra.video.frame@0",
		"terra.video.frame",
	} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseSchemaRef(value); err == nil {
				t.Fatalf("expected %q to be rejected", value)
			}
		})
	}
}
