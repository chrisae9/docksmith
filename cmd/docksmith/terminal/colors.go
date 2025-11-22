package terminal

import "os"

// Red returns ANSI red color code, or empty string if NO_COLOR is set
func Red() string {
	if os.Getenv("NO_COLOR") != "" {
		return ""
	}
	return "\033[31m"
}

// Green returns ANSI green color code, or empty string if NO_COLOR is set
func Green() string {
	if os.Getenv("NO_COLOR") != "" {
		return ""
	}
	return "\033[32m"
}

// Yellow returns ANSI yellow color code, or empty string if NO_COLOR is set
func Yellow() string {
	if os.Getenv("NO_COLOR") != "" {
		return ""
	}
	return "\033[33m"
}

// Gray returns ANSI gray color code, or empty string if NO_COLOR is set
func Gray() string {
	if os.Getenv("NO_COLOR") != "" {
		return ""
	}
	return "\033[90m"
}

// Reset returns ANSI reset code, or empty string if NO_COLOR is set
func Reset() string {
	if os.Getenv("NO_COLOR") != "" {
		return ""
	}
	return "\033[0m"
}
