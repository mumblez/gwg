# gwg
Github Webhook Gateway - WIP

# Goal

- Handling of multiple repository webhooks
    - reuse of base url path
    - read the payload
        - get the repository info and verify with the relevant secret from config
- Force update of local repo from remote repo
- Trigger post tasks
    - touch trigger file for incron / inotify workflow
- Hot reload of configuration without restart


The reuse of the base url path is important, this allows easy management from cloud providers and/or upstream proxies, e.g. there might be some
IP whitelisting based on the initial url path, checking / validating of headers or general load balancer / url map configurations based on the url path.

So to handle and reuse those upstream settings we can set the same url and path on multiple github repositories with different secrets. The
gateway will then map those based on the configuration provided before before we validate, update, trigger post tasks!
