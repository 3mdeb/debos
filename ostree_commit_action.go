package main

import (
	"fmt"
	"log"
	"os"
	"path"

	"github.com/sjoerdsimons/ostree-go/pkg/otbuiltin"
)

type OstreeCommitAction struct {
	*BaseAction
	Repository string
	Branch     string
	Subject    string
	Command    string
}

func (ot *OstreeCommitAction) Run(context *YaibContext) {
	repoPath := path.Join(context.artifactdir, ot.Repository)

	repoDev := path.Join(context.rootdir, "dev")
	os.RemoveAll(repoDev)

	repo, err := otbuiltin.OpenRepo(repoPath)
	if err != nil {
		log.Fatal(err)
	}

	_, err = repo.PrepareTransaction()
	if err != nil {
		log.Fatal(err)
	}

	opts := otbuiltin.NewCommitOptions()
	opts.Subject = ot.Subject
	ret, err := repo.Commit(context.rootdir, ot.Branch, opts)
	if err != nil {
		log.Fatal(err)
	} else {
		fmt.Printf("Commit: %s\n", ret)
	}
	_, err = repo.CommitTransaction()
	if err != nil {
		log.Fatal(err)
	}
}
