package cloudinit

import (
	"testing"
)

func validTemplate() CloudInitTemplate {
	return CloudInitTemplate{
		Name:     "basic",
		UserData: "#cloud-config\npackages:\n  - vim\n",
	}
}

func TestValidateCloudInitTemplate_Valid(t *testing.T) {
	if err := ValidateCloudInitTemplate(validTemplate()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateCloudInitTemplate_MissingName(t *testing.T) {
	tpl := validTemplate()
	tpl.Name = ""
	if err := ValidateCloudInitTemplate(tpl); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateCloudInitTemplate_MissingUserData(t *testing.T) {
	tpl := validTemplate()
	tpl.UserData = ""
	if err := ValidateCloudInitTemplate(tpl); err == nil {
		t.Fatal("expected error for missing userData")
	}
}
