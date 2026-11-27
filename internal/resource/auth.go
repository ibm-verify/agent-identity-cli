package resource

import "net/http"

// AuthDoer sends a request with the connection's bearer token. It satisfies
// the HttpRequestDoer interface of every generated client, so the clients of
// all the resources share it.
type AuthDoer struct {
	client interface {
		Do(*http.Request) (*http.Response, error)
	}
	token string
}

// Do sets the Authorization header and sends the request.
func (d *AuthDoer) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+d.token)
	return d.client.Do(req)
}

// Doer returns the AuthDoer of the connection, to pass to a generated client
// with its WithHTTPClient option. A connection with no token is
// ErrUnauthorized, so no request is sent without one.
func (c Conn) Doer() (*AuthDoer, error) {
	if c.Token == "" {
		return nil, Unauthorized()
	}
	d := &AuthDoer{token: c.Token}
	if c.HTTP != nil {
		d.client = c.HTTP
	} else {
		d.client = http.DefaultClient
	}
	return d, nil
}
