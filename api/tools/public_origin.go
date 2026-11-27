package tools

import "crynux_as/config"

// PublicOrigin returns the configured absolute public AS origin for Image/Video
// result download URLs.
func PublicOrigin() string {
	return config.PublicOrigin()
}
