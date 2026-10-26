package mailtag

import (
	"reflect"
	"testing"
)

func TestKeywordsExcludeStatusAndDeduplicate(t *testing.T) {
	got := Keywords([]string{`\Seen`, `\Flagged`, "$label1", "Project-X", "project-x", "$Junk", "NonJunk", "$Forwarded", "bad tag", "\x1b[31m"})
	want := []string{"$label1", "Project-X"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %q, want %q", got, want)
	}
	if !Has(got, "$LABEL1") {
		t.Fatal("keywords must be case insensitive")
	}
}

func TestKeywordAndNameValidation(t *testing.T) {
	for _, keyword := range []string{"", `\Seen`, "$junk", "two words", "x)", "x\n", "naïve", `x]`, `x{`, `x*`, `x%`, `x"`} {
		if ValidateKeyword(keyword) == nil {
			t.Errorf("accepted invalid keyword %q", keyword)
		}
	}
	for _, keyword := range []string{"$label1", "Project-X", "a_b.c", "a+b", "tag!"} {
		if err := ValidateKeyword(keyword); err != nil {
			t.Errorf("%q: %v", keyword, err)
		}
	}
	for _, name := range []string{"", "  ", "danger\x1b[31m", "direction\u202e", "new\nline"} {
		if ValidateName(name) == nil {
			t.Errorf("accepted invalid name %q", name)
		}
	}
	if err := ValidateName("À faire"); err != nil {
		t.Fatal(err)
	}
}

func TestThunderbirdDefaults(t *testing.T) {
	for keyword, name := range map[string]string{"$label1": "Important", "$label2": "Work", "$label3": "Personal", "$label4": "To Do", "$label5": "Later"} {
		d := Default(7, keyword)
		if d.Name != name || d.Color == "" || d.AccountID != 7 {
			t.Fatalf("default = %+v", d)
		}
	}
	if d := Default(7, "custom"); d.Name != "custom" || d.Color != "" {
		t.Fatalf("custom = %+v", d)
	}
}
