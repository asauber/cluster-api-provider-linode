package machine

import (
	"context"
	"os"

	"github.com/linode/linodego"
	"golang.org/x/oauth2"
)

// tokenSource contains API token for Linode API.
type tokenSource struct {
	AccessToken string
}

// Token returns new oauth2 object with DO API token.
func (t *tokenSource) Token() (*oauth2.Token, error) {
	token := &oauth2.Token{
		AccessToken: t.AccessToken,
	}
	return token, nil
}

// getLinodeClient creates new linodego client used to interact with the Linode API.
func getLinodeClient() *linodego.Client {
	token := &tokenSource{
		AccessToken: os.Getenv("LINODE_TOKEN"),
	}
	oc := oauth2.NewClient(context.Background(), token)
	return linodego.NewClient(oc)
}
