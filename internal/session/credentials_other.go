//go:build !darwin

package session

// LoggedIn reports whether Claude has stored credentials for the profile.
func LoggedIn(profile string) bool { return hasCredentialsFile(profile) }
