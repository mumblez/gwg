package main

import (
	"fmt"
	"os"

	"gopkg.in/src-d/go-git.v4"
	. "gopkg.in/src-d/go-git.v4/_examples"
)

// Pull changes from a remote repository
func main() {
	CheckArgs("<path>")
	path := os.Args[1]

	// We instance a new repository targeting the given path (the .git folder)
	r, err := git.PlainOpen(path)
	CheckIfError(err)

	// Get the working directory for the repository
	w, err := r.Worktree()
	CheckIfError(err)

	// Pull the latest changes from the origin remote and merge into the current branch
	// Info("git pull origin")
	// err = w.Pull(&git.PullOptions{RemoteName: "origin"})
	// CheckIfError(err)

	// fetch (origin by default)
	r.Fetch(&git.FetchOptions{RemoteName: "origin/master"})

	// Print the latest commit that was just pulled
	ref, err := r.Head()
	CheckIfError(err)

	// git reset --hard origin/master
	w.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: ref.Hash()})
	err = w.Pull(&git.PullOptions{RemoteName: "origin"})
	CheckIfError(err)

	commit, err := r.CommitObject(ref.Hash())
	CheckIfError(err)

	fmt.Println(commit)
}
