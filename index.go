package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	gapi "github.com/google/go-github/github"
	"golang.org/x/oauth2"
	"gopkg.in/go-playground/webhooks.v5/github"
)

var hook *github.Webhook
var cli *gapi.Client

func init() {
	var err error
	hook, err = github.New(github.Options.Secret(os.Getenv("WH_SECRET")))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GH_TOKEN")},
	)
	tc := oauth2.NewClient(ctx, ts)
	cli = gapi.NewClient(tc)
}

const tagPrefix = "refs/tags/"

func Handler(w http.ResponseWriter, r *http.Request) {
	payload, err := hook.Parse(r, github.PushEvent)
	if err != nil {
		if err == github.ErrEventNotFound {
			fmt.Printf("hook.Parse: %s\n", err)
			return
		}
	}

	switch payload.(type) {
	case github.PushPayload:
		push := payload.(github.PushPayload)
		fmt.Printf("push: %+v\n", push)
		if !strings.HasPrefix(push.Ref, tagPrefix) || !push.Created {
			fmt.Printf("not tag push")
		}
		tag := strings.TrimPrefix(push.Ref, tagPrefix)

		title := fmt.Sprintf("Possibly erroneous tag pushed: %s", tag)
		body := fmt.Sprintf(`Woof Woof :dog:

@%s looks like you pushed old tag into this repo.
Remove this tag by running `+"`git tag -d %s && git push origin :%[2]s`"+`

This probably happened because your local git repositories still have old tags.
You can remove all of them in one go by running:
 `+"`find libp2p multiformats ipfs -maxdepth 1 -mindepth 1 -type d | while read dir; do; cd $dir; git fetch --prune origin '+refs/tags/*:refs/tags/*'; cd ../..; done` in `$GOPATH/src/github.com/`."+`

Yours truly, with :poodle:, Tag Dog.
`, push.Sender.Login, tag)

		ir := &gapi.IssueRequest{
			Title:     &title,
			Body:      &body,
			Assignees: &[]string{push.Sender.Login},
		}
		iss, resp, err := cli.Issues.Create(context.Background(), push.Repository.Owner.Login,
			push.Repository.Name, ir)
		if err != nil {
			fmt.Printf("creating issue: %s\n", err)
			return
		}
		fmt.Printf("created issue: %+v %+v\n", iss, resp)
	default:
		fmt.Printf("unknown type: %+v\n", payload)
	}
}
