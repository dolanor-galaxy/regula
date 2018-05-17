package client_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/heetch/rules-engine/api/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Example() {
	c, err := client.NewClient("http://127.0.0.1:5331")
	if err != nil {
		log.Fatal(err)
	}

	list, err := c.ListRulesets(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	for _, e := range list {
		e.Ruleset.Eval(nil)
	}
}

func TestClient(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error": "some err"}`)
		}))
		defer ts.Close()

		cli, err := client.NewClient(ts.URL)
		require.NoError(t, err)

		_, err = cli.ListRulesets(context.Background())
		require.EqualError(t, err, "some err")
	})

	t.Run("ListRulesets", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.NotEmpty(t, r.Header.Get("User-Agent"))
			assert.Equal(t, "application/json", r.Header.Get("Accept"))
			fmt.Fprintf(w, `[{"name": "a"}]`)
		}))
		defer ts.Close()

		cli, err := client.NewClient(ts.URL)
		require.NoError(t, err)

		rs, err := cli.ListRulesets(context.Background())
		require.NoError(t, err)
		require.Len(t, rs, 1)
	})

}