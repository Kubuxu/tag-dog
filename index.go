package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	logger "github.com/apsdehal/go-logger"
	gapi "github.com/google/go-github/v24/github"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"gopkg.in/go-playground/webhooks.v5/github"
)

var hook *github.Webhook
var cli *gapi.Client
var log *logger.Logger
var start sync.Once

func firstRun() {
	var err error
	hook, err = github.New(github.Options.Secret(os.Getenv("WH_SECRET")))
	if err != nil {
		panic(err)
	}
	log, _ = logger.New("tag-dog", 0) //nolint: gosec
	log.SetFormat("%{lvl}: %{message} %{file}:%{line}")

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GH_TOKEN")},
	)
	tc := oauth2.NewClient(ctx, ts)
	cli = gapi.NewClient(tc)
}

const tagPrefix = "refs/tags/"

var _ = Handler // used by now.sh, trick linters that it is used

// Handler is run by now.sh
func Handler(w http.ResponseWriter, r *http.Request) {
	start.Do(firstRun)

	payload, err := hook.Parse(r, github.PushEvent)
	if err != nil {
		if err == github.ErrEventNotFound {
			log.Noticef("hook.Parse: %s", err)
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch push := payload.(type) {
	case github.PushPayload:
		owner := push.Repository.Owner.Login
		name := push.Repository.Name

		if !strings.HasPrefix(push.Ref, tagPrefix) || !push.Created {
			log.Debugf("not tag push: %s/%s@%s", owner, name, push.Ref)
			return
		}
		tag := strings.TrimPrefix(push.Ref, tagPrefix)

		var allrefs []*gapi.Reference
		refsopt := &gapi.ReferenceListOptions{
			Type:        "tags",
			ListOptions: gapi.ListOptions{PerPage: 50},
		}
		for {
			refs, resp, err := cli.Git.ListRefs(ctx, owner, name, refsopt)
			if err != nil {
				log.Errorf("fetching refs: %s", err)
				return
			}
			allrefs = append(allrefs, refs...)
			if resp.NextPage == 0 {
				break
			}
			refsopt.Page = resp.NextPage
		}
		found := false
		for _, ref := range allrefs {
			if ref.GetRef() == (tagPrefix + "gx/" + tag) {
				if o := ref.GetObject(); o != nil {
					if o.GetSHA() == push.After {
						found = true
						break
					}
				}
			}
		}
		if !found {
			log.Debug("didn't find matching tag")
			return
		}

		err := createIssue(ctx, tag, push)
		if err != nil {
			log.Errorf("creating issue: %s", err)
			return
		}
		log.Infof("created issue")
	default:
		log.Debugf("unknown type: %+v", payload)
	}
}

func createIssue(ctx context.Context, tag string, push github.PushPayload) error {
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
	_, _, err := cli.Issues.Create(ctx, push.Repository.Owner.Login,
		push.Repository.Name, ir)
	if err != nil {
		return errors.Wrap(err, "creating issue")
	}
	return nil
}
