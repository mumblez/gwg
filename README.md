# gwg
Github Webhook Gateway - WIP...

# Goal

To update multiple git repositories when changes are pushed to github WITHOUT polling!

- Handling of multiple repository webhooks
    - one webhook path per repo (on same URL)
    - read the payload
        - get the repository info and verify with the relevant secret from config
- Force update of local repo from remote repo
    - essentially `git fetch; git reset --hard $SHA # latest remote commit
- Trigger post tasks
    - touch trigger file for incron / inotify workflow
- Hot reload of configuration without restart
- Can be used as part of CI / CD, assuming the server is publicly accessible and github can send a post request.
    - No need to poll for updates!


# Workflow

1. Webhook request from github, e.g. a push event with json payload
2. Check the URL path against any repos we have in our configuration, if a match is found:
    - Validate the payload against the webhook secret
    - Validate the git url in payload against our config
    - Respond to only github push events
    - Before we proceed to update, we do a final check against of the git url and branch against our config
3. If the repository doesn't exist locally we'll do an initial clone, else we'll update
4. Touch trigger file (if set), enough for inotify event and/or incron to trigger post tasks


# Configuration

```yaml
listen: localhost                           # leave blank or remove to accept connections on all interfaces
port: 5555                                  # specify a port above 1024 to run as a non root user
retry_count: 10                             # number of times to attempt a fetch, (fetches from github can be flaky sometimes)
retry_delay: 1                              # delay between retries
initialise: true                            # clone the repositories if they don't exist locally (on startup and when new repo's added to config (hot-reload))
logging:
  format: text                              # [text|json] defaults to text or json if not recognised
  output: stdout                            # [stdout|/path/to/file] defaults to stdout
  level: info                               # [debug|info|warn|error] defaults to info
  timestamp: true                           # [true|false] display timestamp or not, defaults to true
repos:
    ### required ###
  - url: git@github.com:ns/repo-1.git
    path: /gwg/repo-1                       # the same path used to setup the webhook
    directory: /path/to/local/repo
    ### optional ###
    branch: master                          # defaults to master
    remote: origin                          # defaults to origin
    trigger: /path/to/trigger/file          # the file to `touch` after a successful update
    secret: webhookPassword                 # the secret password used to setup the webhook
    sshPrivKey: /path/to/private/key        # leave blank or remove field if public repository
    sshPassPhrase: sshPassPhrase-123        # leave blank or remove field if no passphrase
  - url: git@github.com:ns/repo-2.git
    path: /gwg/repo-2
    directory: /path/to/clone/to-2
    branch: master
    trigger: /path/to/trigger/file-2
    secret: superSecret-repo-2
    sshPrivKey: /path/to/priv/key-2
    sshPassPhrase: abc
    # append more repos ...
```

The program will search for a `config.[yaml,json,toml]` file in:
- `/etc/gwg`
- `.` - current directory

Choose the format of your choice, yaml, json or toml.

# Setup

- create system user and group, e.g. for gwg - `useradd -r -b /etc -U gwg` # will set home to /etc/gwg but directory still needs to be created
- create and secure config dir - `mkdir /etc/gwg && chown gwg:gwg /etc/gwg && chmod 770 /etc/gwg`
- ensure user and/or group can write to repository locations, read ssh private keys and trigger files
    - ensure trigger files exist if you want / have post update tasks
- add config to `/etc/gwg`, e.g. `/etc/gwg/config.yaml` or current directory of executable
    - ensure only gwg user can read config file
- start server as newly created user!, if you have a systemd system, see the gwg.service sample file and instructions below

# Notes

## Hot Reloading
The configuration file can be editted and it will be hot-reloaded, the only exception is if you need you update the `listen` and `port` fields as they will require a restart!

## Update method
If the repository does not exist locally it will be cloned

If the repository already exists, it will be updated, equivalent to:

```sh
git fetch origin
git reset --hard $LATEST_REMOTE_COMMIT_ON_SPECIFIC_BRANCH
```
This means we will always trust the remote over our local repository, it also means we avoid any potential merge conflicts as we do a hard reset!

## Logging
If you want systemd to handle logs with journalctl, you can set:
```yaml
...
logging:
  format: text
  output: stdout
  timestamp: false
  ...
```
systemd will capture stdout and will also add a timestamp so best to set to `false`.

## basic systemd service config
```
[Unit]
Description=Github Webhook Gateway
After=syslog.target network.target

[Service]
User=gwg
Group=gwg
WorkingDirectory=/etc/gwg
ExecStart=/usr/local/bin/gwg
Restart=always

[Install]
WantedBy=multi-user.target
```

Save as `/etc/systemd/system/gwg.service`

```sh
systemctl daemon-reload
systemctl enable gwg
systemctl start # assuming you already have a configuration /etc/gwg/config.yaml
```



# TODO
- gc / prune deleted repos?
- add mutexes around updates and reloads?
- add raw shell exec after update?
- add slack notifications on errors
- add cli flags and env vars
- refactor

.
.
.
.
.
.
.
.
.
.
