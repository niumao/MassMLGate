package casbin

type MenuItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Path  string `json:"path"`
}

// allMenuItems is the full menu; FilterMenu keeps only what the caller's
// domain is allowed to read (menu:{key}, read), per the design spec.
var allMenuItems = []MenuItem{
	{Key: "dashboard", Label: "Dashboard", Path: "/dashboard"},
	{Key: "tenants", Label: "Tenants", Path: "/tenants"},
	{Key: "users", Label: "Users", Path: "/users"},
	{Key: "teams", Label: "Teams", Path: "/teams"},
	{Key: "workspaces", Label: "Workspaces", Path: "/workspaces"},
	{Key: "size_presets", Label: "Size Presets", Path: "/size-presets"},
	{Key: "token_quotas", Label: "Token Quotas", Path: "/token-quotas"},
	{Key: "test_runs", Label: "Test Runs", Path: "/test-runs"},
}

func (e *Enforcer) FilterMenu(userID, domain string) []MenuItem {
	var out []MenuItem
	for _, item := range allMenuItems {
		ok, err := e.Enforce(userID, domain, "menu:"+item.Key, "read")
		if err == nil && ok {
			out = append(out, item)
		}
	}
	return out
}
