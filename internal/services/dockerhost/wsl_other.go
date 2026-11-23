//go:build !windows

package dockerhost

import "fmt"

// wslKeeper holds nothing: WSL exists only on Windows.
type wslKeeper struct{}

// keepWSLDistribution keeps nothing: WSL exists only on Windows.
func (*Service) keepWSLDistribution(string) {}

// validateWSLDistribution refuses any distro: WSL exists only on Windows.
func validateWSLDistribution(_, name string) error {
	if name != "" {
		return fmt.Errorf("%w: WSL needs a router running on Windows", ErrInvalidSettings)
	}
	return nil
}
