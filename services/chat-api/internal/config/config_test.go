package config

import "testing"

func TestLoadReadsServiceNow(t *testing.T) {
	t.Setenv("KA_SERVICENOW_MODE", "on")
	t.Setenv("KA_SERVICENOW_URL", "https://dev12345.service-now.com")
	t.Setenv("KA_SERVICENOW_USER", "svc")
	t.Setenv("KA_SERVICENOW_PASSWORD", "pw")
	t.Setenv("KA_TEAM_METADATA", `{"star":{"serviceNowGroup":"Star Reporting"}}`)
	c := Load()
	if c.ServiceNowMode != "on" || c.ServiceNowURL == "" || c.ServiceNowUser != "svc" {
		t.Errorf("ServiceNow config not loaded: %+v", c)
	}
	if c.ServiceNowLimit != 5 {
		t.Errorf("ServiceNowLimit default = %d, want 5", c.ServiceNowLimit)
	}
	if c.TeamMetadata == "" {
		t.Error("TeamMetadata not loaded from KA_TEAM_METADATA")
	}
}
