// Package auth implements the account model, JWT access tokens, and
// refresh-token rotation (planner tasks G-13..G-15). 2FA/TOTP (G-16),
// device-identity integration with rendezvous's AuthorizeDevice (G-17),
// rate limiting (G-18), and audit events (G-19) are not implemented yet.
package auth
