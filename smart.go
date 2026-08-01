package main

import (
	"net/url"
	"strings"
)

// DetectSmartPaths analyzes the target URL and generates the most effective attack paths.
func DetectSmartPaths(rawURL string) *SmartPaths {
	u, _ := url.Parse(rawURL)

	paths := &SmartPaths{
		LoginGET:  u.String(),
		LoginPOST: u.String(),
		Dashboard: u.String(),
		Profile:   u.String(),
	}

	// Clean base URL for path construction
	baseURL := u.Scheme + "://" + u.Host

	// Extract significant paths from the URL
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")

	// Detect common CMS/Script patterns
	hasSMS := false
	hasAdmin := false
	hasAPI := false
	hasUser := false

	for _, part := range pathParts {
		partLower := strings.ToLower(part)
		switch {
		case strings.Contains(partLower, "sms"):
			hasSMS = true
		case strings.Contains(partLower, "admin"):
			hasAdmin = true
		case strings.Contains(partLower, "api"):
			hasAPI = true
		case strings.Contains(partLower, "user"):
			hasUser = true
		}
	}

	// Generate login paths based on detected patterns
	if hasSMS {
		prefix := strings.Join(pathParts, "/")
		paths.LoginGET = baseURL + "/" + prefix + "/SignIn"
		paths.LoginPOST = baseURL + "/" + prefix + "/signmein"
		paths.Dashboard = baseURL + "/" + prefix + "/test/"
		paths.Profile = baseURL + "/" + prefix + "/test/Profile"
	} else if hasAdmin {
		prefix := strings.Join(pathParts, "/")
		paths.LoginGET = baseURL + "/" + prefix + "/login"
		paths.LoginPOST = baseURL + "/" + prefix + "/login"
		paths.Dashboard = baseURL + "/" + prefix + "/dashboard"
		paths.Profile = baseURL + "/" + prefix + "/profile"
	} else if hasAPI {
		prefix := strings.Join(pathParts, "/")
		paths.LoginGET = baseURL + "/" + prefix + "/auth/login"
		paths.LoginPOST = baseURL + "/" + prefix + "/auth/login"
		paths.Dashboard = baseURL + "/" + prefix + "/dashboard"
		paths.Profile = baseURL + "/" + prefix + "/me"
	} else if hasUser {
		prefix := strings.Join(pathParts, "/")
		paths.LoginGET = baseURL + "/" + prefix + "/login"
		paths.LoginPOST = baseURL + "/" + prefix + "/login"
		paths.Dashboard = baseURL + "/" + prefix + "/account"
		paths.Profile = baseURL + "/" + prefix + "/profile"
	} else {
		// Generic web app detection — try common paths
		paths.LoginGET = baseURL + "/login"
		paths.LoginPOST = baseURL + "/login"
		paths.Dashboard = baseURL + "/dashboard"
		paths.Profile = baseURL + "/profile"

		// Check if the URL itself is a login page
		if strings.Contains(strings.ToLower(u.Path), "login") || strings.Contains(strings.ToLower(u.Path), "signin") {
			paths.LoginGET = u.String()
			paths.LoginPOST = u.String()
		}
	}

	// Detect admin path if exists
	if hasAdmin {
		prefix := strings.Join(pathParts, "/")
		paths.AdminPath = baseURL + "/" + prefix + "/"
	}

	return paths
}
