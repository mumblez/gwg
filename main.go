package main

import (
	"fmt"
	"os"

	"gopkg.in/src-d/go-git.v4"
	. "gopkg.in/src-d/go-git.v4/_examples"
	"gopkg.in/src-d/go-git.v4/plumbing"
	// "gopkg.in/src-d/go-git.v4/plumbing/storer"
)

// Pull changes from a remote repository
func main() {
	// TODO
	// - break out into function
	// pass in path (repo dir), ssh priv key location
	// - create ssh auth connection
	CheckArgs("<path>")
	path := os.Args[1]

	// We instance a new repository targeting the given path (the .git folder)
	r, err := git.PlainOpen(path)
	CheckIfError(err)

	// Get the working directory for the repository
	w, err := r.Worktree()
	CheckIfError(err)

	// Fetch latest commits
	fmt.Println("Fetching...")
	err = r.Fetch(&git.FetchOptions{RemoteName: "origin"})
	if err == git.NoErrAlreadyUpToDate {
		fmt.Println("No new commits")
		os.Exit(0)
	}

	// Get local and remote refs to compare hashes before we proceed
	remoteRef, err := r.Reference(plumbing.ReferenceName("refs/remotes/origin/master"), true)
	//remoteRef, err := r.Reference(plumbing.ReferenceName("refs/remotes/origin/HEAD"), true)
	CheckIfError(err)
	localRef, err := r.Reference(plumbing.ReferenceName("HEAD"), true)
	CheckIfError(err)
	// Exit if there's no need to update
	if remoteRef.Hash() == localRef.Hash() {
		fmt.Println("Already up to date")
		os.Exit(0)
	}

	// git reset --hard [origin/master|hash]
	fmt.Println("Resetting work tree....")
	err = w.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: remoteRef.Hash()})
	CheckIfError(err)

	// confirm changes
	fmt.Println("Confirming changes....")
	headRef, _ := r.Reference(plumbing.ReferenceName("HEAD"), true)
	CheckIfError(err)
	if headRef.Hash() == remoteRef.Hash() {
		fmt.Println("Success")
	}

	// print latest commit
	commit, err := r.CommitObject(headRef.Hash())
	CheckIfError(err)
	fmt.Println(commit)
	fmt.Println(headRef.Hash())

}
