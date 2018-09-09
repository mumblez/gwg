package main

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/go-github/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/ssh"
)

type config struct {
	Listen     string `mapstructure:"listen"`
	Port       string `mapstructure:"port"`
	RetryCount int    `mapstructure:"retry_count"`
	RetryDelay int    `mapstructure:"retry_delay"`
	Initialise bool   `mapstructure:"initialise"`
	Logging    logger
	Logfile    *os.File
	LastUpdate time.Time
	Repos      []repo
}

type logger struct {
	Format    string `mapstructure:"format"`
	Output    string `mapstructure:"output"`
	Level     string `mapstructure:"level"`
	Timestamp bool   `mapstructure:"timestamp"`
}

type repo struct {
	URL           string `mapstructure:"url"`
	Path          string `mapstructure:"path"`
	Directory     string `mapstructure:"directory"`
	Label         string `mapstructure:"label"`
	LabelType     string `mapstructure:"labelType"`
	Remote        string `mapstructure:"remote"`
	Secret        string `mapstructure:"secret"`
	SSHPrivKey    string `mapstructure:"sshPrivKey"`
	SSHPassPhrase string `mapstructure:"sshPassPhrase"`
	Trigger       string `mapstructure:"trigger"`
}

// C is global config
var C config
var log = logrus.New()

func (c *config) FindRepo(path string) (int, bool) {
	for r, repo := range c.Repos {
		if repo.Path == cleanURL(path) {
			return r, true
		}
	}
	return 0, false
}

func cleanURL(url string) string {
	// strip trailing slash
	if url[len(url)-1] == '/' {
		return url[:len(url)-1]
	}
	return url
}

func (r *repo) clone() {

	rlog := log.WithFields(logrus.Fields{
		"repo":      r.Name(),
		"path":      r.Path,
		"label":     r.Label,
		"labelType": r.LabelType,
	})
	sshAuth, err := ssh.NewPublicKeysFromFile("git", r.SSHPrivKey, r.SSHPassPhrase)
	if err != nil {
		rlog.Errorf("Failed to setup ssh auth: %v", err)
		return
	}

	// do a vanilla clone and checkout relevant tag / branch
	repo, err := git.PlainClone(r.Directory, false, &git.CloneOptions{
		URL: r.URL,
		//ReferenceName: plumbing.ReferenceName("refs/heads/" + r.Branch),
		//SingleBranch: true,
		// tag mode = AllTags
		Auth: sshAuth,
		Tags: git.AllTags,
	})
	if err != nil {
		rlog.Errorf("Failed to clone repository: %v", err)
		return
	}

	tree, err := repo.Worktree()
	if err != nil {
		rlog.Errorf("Failed to open work tree for repository: %v", err)
		return
	}

	var ref string
	if r.LabelType == "tag" {
		ref = "refs/tags/" + r.Label
	} else {
		ref = "refs/remotes/" + r.Remote + "/" + r.Label
	}

	err = tree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName(ref),
	})
	if err != nil {
		rlog.Errorf("Failed to checkout %s: %v", r.Label, err)
		return
	}

	rlog.Info("Cloned repository")

	r.touchTrigger()
}

// essentially git fetch and git reset --hard origin/master | latest remote commit
func (r *repo) update() {
	rlog := log.WithFields(logrus.Fields{
		"repo":      r.Name(),
		"path":      r.Path,
		"remote":    r.Remote,
		"label":     r.Label,
		"labelType": r.LabelType,
	})
	sshAuth, err := ssh.NewPublicKeysFromFile("git", r.SSHPrivKey, r.SSHPassPhrase)
	if err != nil {
		rlog.Errorf("Failed to setup ssh auth: %v", err)
		return
	}

	repo, err := git.PlainOpen(r.Directory)
	if err != nil {
		rlog.Errorf("Failed to open local git repository: %v", err)
		return
	}

	w, err := repo.Worktree()
	if err != nil {
		rlog.Errorf("Failed to open work tree for repository: %v", err)
		return
	}

	// fetches from github can be flaky, sometimes we get a blank .git/refs/remotes/[master|branch name],
	// and complaints about broken refs, subsequent fetches should fix this!
	// we'll fetch up to the retry amount until it succeeds!.

	for i := 0; i < C.RetryCount; i++ {
		rlog.Info("Fetch attempt: ", i+1)
		err = repo.Fetch(&git.FetchOptions{
			RemoteName: r.Remote,
			Auth:       sshAuth,
			Force:      true,
			Tags:       git.AllTags,
		})
		if err == nil {
			break
		}
		if err == git.NoErrAlreadyUpToDate {
			rlog.Info("No new commits")
			return
		}
		if err != nil {
			rlog.Errorf("Failed to fetch updates: %v", err)
			time.Sleep(time.Duration(C.RetryDelay) * time.Second)
			continue
		}
	}
	rlog.Info("Fetched new updates")

	var ref string
	if r.LabelType == "tag" {
		ref = "refs/tags/" + r.Label
	} else {
		ref = "refs/remotes/" + r.Remote + "/" + r.Label
	}

	// Get local and remote refs to compare hashes before we proceed
	remoteRef, err := repo.Reference(plumbing.ReferenceName(ref), true)
	if err != nil {
		rlog.Errorf("Failed to get reference for %s: %v", ref, err)
		return
	}
	localRef, err := repo.Reference(plumbing.ReferenceName("HEAD"), true)
	if err != nil {
		rlog.Errorf("Failed to get local reference for HEAD: %v", err)
		return
	}

	if remoteRef.Hash() == localRef.Hash() {
		rlog.Warning("Already up to date")
		return
	}

	// git reset --hard [origin/master|hash] - works for both branch and tag, we'll reset direct to the hash
	err = w.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: remoteRef.Hash()})
	if err != nil {
		rlog.Errorf("Failed to hard reset work tree: %v", err)
		return
	}
	rlog.Info("Hard reset successful, confirming changes....")
	headRef, err := repo.Reference(plumbing.ReferenceName("HEAD"), true)
	if err != nil {
		rlog.Errorf("Failed to get local HEAD reference: %v", err)
		return
	}

	if headRef.Hash() == remoteRef.Hash() {
		rlog.Infof("Changes confirmed, latest hash: %v", headRef.Hash())
	} else {
		rlog.Error("Something went wrong, hashes don't match!")
		rlog.Debugf("Remote hash: %v", remoteRef.Hash())
		rlog.Debugf("Local hash:  %v", headRef.Hash())
		return
	}

	r.touchTrigger()
}

func (r *repo) touchTrigger() {
	rlog := log.WithFields(logrus.Fields{
		"repo":      r.Name(),
		"path":      r.Path,
		"label":     r.Label,
		"labelType": r.LabelType,
	})
	if r.HasTrigger() {
		if err := os.Chtimes(r.Trigger, time.Now(), time.Now()); err != nil {
			rlog.Errorf("Failed to update trigger file: %v, attempting to create...", err)

			// attempt to create trigger file silently, reports error but creates empty file
			os.OpenFile(r.Trigger, os.O_RDONLY|os.O_CREATE, 0660)
			if _, err := os.Stat(r.Trigger); err != nil {
				rlog.Errorf("Failed to create trigger file: %v", err)
			}
			rlog.Info("Successfully created trigger file")
			return
		}
		rlog.Info("Successfully updated trigger file")
	}
}

func (c *config) validatePathsUniq() {
	paths := make(map[string]bool)

	for _, r := range c.Repos {
		if _, ok := paths[r.Path]; ok {
			// duplicate found
			log.Errorf("Multiple repos found with the same path: %v, please correct, only the first instance will be used otherwise", r.Path)
		}
		paths[r.Path] = true
	}
}

// short name for the logs
func (r *repo) Name() string {
	return strings.TrimSuffix((strings.TrimPrefix(r.URL, "git@github.com:")), ".git")
}

func isEmpty(field string) bool {
	if len(field) == 0 {
		return true
	}
	return false
}

func (r *repo) HasTrigger() bool {
	if isEmpty(r.Trigger) {
		return false
	}
	return true
}

func (r *repo) HasSecret() bool {
	if isEmpty(r.Secret) {
		return false
	}
	return true
}

func handler(w http.ResponseWriter, r *http.Request) {
	idx, ok := C.FindRepo(r.URL.Path)
	if !ok {
		log.Warnf("Repository not found for path: %v", r.URL.Path)
		return
	}

	// create separate repo var incase it changes on us (hot reloading)
	// TODO add mutex and don't use copy
	var repo = C.Repos[idx]

	payload, err := github.ValidatePayload(r, []byte(repo.Secret))
	defer r.Body.Close()
	if err != nil {
		log.Errorf("Error validating request body: %v", err)
		return
	}

	event, err := github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		log.Errorf("Could not parse webhook: %v", err)
		return
	}

	switch e := event.(type) {
	case *github.PushEvent:
		if repo.URL == *e.Repo.SSHURL && (repo.Label == strings.TrimPrefix(*e.Ref, "refs/heads/") || repo.Label == strings.TrimPrefix(*e.Ref, "refs/tags/")) {
			// TODO: add mutex incase in the middle of an update
			go repo.update()
		} else {
			log.WithFields(logrus.Fields{
				"URL": *e.Repo.SSHURL,
				"Ref": *e.Ref,
			}).Warn("Push event did not match our configuration")
		}
		return
	default:
		log.Warnf("Unknown event type %v", github.WebHookType(r))
		return
	}
}

func (c *config) setRepoDefaults() {
	for i := range c.Repos {
		if c.Repos[i].LabelType == "" {
			c.Repos[i].LabelType = "branch"
		}
		if c.Repos[i].Label == "" {
			c.Repos[i].Label = "master"
		}
		if c.Repos[i].Remote == "" {
			c.Repos[i].Remote = "origin"
		}
	}
}

func (c *config) validateLabelType() {
	for i := range c.Repos {
		// either known or blank, if blank our setRepoDefaults function will set
		if c.Repos[i].LabelType == "branch" || c.Repos[i].LabelType == "tag" || c.Repos[i].LabelType == "" {
			continue
		} else {
			log.Warnf("Unknown label type for repo: %s, defaulting to branch", c.Repos[i].Name)
		}

	}
}

func (c *config) setLogging() {

	// inverse timestamp
	var dts bool
	if c.Logging.Timestamp {
		dts = false
	} else {
		dts = true
	}

	if c.Logging.Format == "" || c.Logging.Format == "text" {
		log.Formatter = &logrus.TextFormatter{FullTimestamp: true, DisableTimestamp: dts}
	} else {
		log.Formatter = &logrus.JSONFormatter{DisableTimestamp: dts}
	}

	switch c.Logging.Level {
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}
	// file or stdout
	if c.Logging.Output == "" || c.Logging.Output == "stdout" {
		if c.Logfile != nil {
			c.Logfile.Close()
			c.Logfile = nil
		}
		log.Out = os.Stdout
	} else {
		if c.Logfile != nil {
			if err := c.Logfile.Close(); err != nil {
				log.Errorf("Error closing logfile handle = %+v", err)
			}
		}
		file, err := os.OpenFile(c.Logging.Output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0660)
		if err != nil {
			log.Out = os.Stdout
			log.Errorf("Failed to log to file, using default stdout: %v", err)
			return
		}
		c.Logfile = file
		log.Out = c.Logfile
	}
}

func (c *config) refreshTasks() {
	c.setLogging()
	c.validatePathsUniq()
	c.validateLabelType()
	c.setRepoDefaults()
	c.LastUpdate = time.Now()

	// TODO: initialise repos
	if c.Initialise {
		for _, r := range c.Repos {
			if _, err := os.Stat(r.Directory); err != nil {
				// TODO: throttle with semaphore in future
				go r.clone()
			}
		}
	}
}

func main() {
	// setup config
	viper.SetConfigName("config")
	viper.AddConfigPath("/etc/gwg")
	viper.AddConfigPath(".")

	viper.SetDefault("listen", "0.0.0.0")
	viper.SetDefault("port", 5555)
	viper.SetDefault("retry_delay", 10)
	viper.SetDefault("retry_count", 1)
	viper.SetDefault("initialise", true)
	viper.SetDefault("logging.format", "text")
	viper.SetDefault("logging.output", "stdout")
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.timestamp", true)

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}
	if err := viper.Unmarshal(&C); err != nil {
		log.Fatalf("Failed to setup configuration: %v", err)
	}

	C.refreshTasks()

	// hot reloading can be improved, (adding mutexes might be overkill for now)
	viper.WatchConfig()
	// event fired twice on linux but once on mac? wtf!!!
	viper.OnConfigChange(func(e fsnotify.Event) {
		// log.Infof("op: %+v", e.Op)
		// fires off CREATE and WRITE on linux
		// fires off CREATE on mac
		// both using vim, both creates swp files by default, hmmmm
		// ignore WRITE, but we won't catch changes if things are echo'd directly into file!
		// normal use case will be to open and edit file, so we'll ignore WRITE events for now
		// if e.Op != fsnotify.Create {
		// 	return
		// }

		// alt method
		// seems to work well, we can tweak the timing but quarter second seems ideal
		// 1 second = 1000917642 nanoseconds
		// quarter second = 250229410
		// if time now is less then quarter second of last update, return
		if time.Since(C.LastUpdate).Nanoseconds() < 250229410 {
			return
		}

		// create entirely new config, set defaults and change 'C'
		// yaml and toml differences in repo mappings means we have to unmarshal
		// everything first.
		var newC config
		if err := viper.Unmarshal(&newC); err != nil {
			log.Fatalf("Failed to setup new configuration: %v", err)
		}

		log.Warnf("Config file changed: %v", e.Name)
		log.Debugf("Event: %v", e.Op)
		newC.refreshTasks()

		// replace current config with new one
		C = newC
		log.Warn("Configuration updated")

	})

	// Start the server.
	// (listen and port changes require a restart)
	http.HandleFunc("/", handler)
	http.ListenAndServe(C.Listen+":"+C.Port, nil)

}
