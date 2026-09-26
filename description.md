# MCP resources: issue://<id> and the open list, so a host reads an issue into context rather than pasting it

## Why

`git issue mcp` (1k3x9sp6h12ks5c0pxn0) gives an agent the tracker as tools: it acts through them.
MCP also has resources, which are what a host reads INTO context -- the agent does not call
anything, the host offers "the issue you are working on" as a thing to attach, an editor lists
them in a picker, a subscription says when one changed. Tools are how an agent changes an issue;
resources are how one is in front of it. Today the host has neither a URI for an issue nor a
listing of them, so a person pastes `issue_show` output.

## Shape

**One URI scheme, `issue://`.**

    issue://<xidr>            one issue, as a person reads it: title, body, labels, status,
                              linked commits, relations, the discussion in order, attachments
                              by name -- the text `issue_show` answers, mimeType text/markdown
    issue://<xidr>/meta       the same issue as data: `issue_show`'s structured content,
                              application/json
    issue://                  the open issues, one line each as `list` prints them, text/plain

An `<xidr>` in a URI is the full id: a resource is a stable name, and a prefix is not one. The
server answers a prefix anyway -- an agent will type one -- and a host that lists resources gets
full ids.

**A resource template, `issue://{id}`**, so a host that offers resources by pattern can, and
**completion** on its `{id}`: `completion/complete` answers the ids a prefix matches, which is
what makes typing an id in a host's picker work.

**resources/list is the open issues**, each with its title as the name and `issue://<xidr>` as
the URI; closed ones are reachable by URI and not listed, as `list` does not list them. A host
paging through them gets the SDK's pages.

**Notifications from what the server did.** A tool that created, closed, reopened or pulled sends
`notifications/resources/list_changed`; one that edited, commented on or labelled an issue sends
`notifications/resources/updated` for its URI to whoever subscribed. The server knows what its
own tools did and nothing else: a `git issue comment` run in a shell beside it, or a pull from
another clone, is invisible until the next read. That is stated, not papered over -- the
tracker is refs in a repository, and there is no event to subscribe to short of a filesystem
watch on `.git/refs/git-issues/`, which is the one thing that would close the gap and is not
done here.

## What it takes

1. **Reading.** `Server.AddResource` for `issue://` and `Server.AddResourceTemplate` for
   `issue://{id}` and `issue://{id}/meta`, each handler over `ops.Show` and `ops.List` --
   the same functions the tools call, rendered by the same writers, so a resource and a tool
   answer the same bytes for the same issue. An unknown id is `ResourceNotFound`, as the SDK
   spells it.
2. **Listing.** `resources/list` from `ops.List(open)`; the SDK pages it.
3. **Completion.** `ServerOptions.CompletionHandler` for the template's `id`: `FindRef` per
   candidate is one ref lookup each, over the ids `ListRefs` answers, filtered by prefix.
4. **Notifications.** Each tool handler, after its op returns, calls the server's
   `ResourceUpdated` / `ResourceListChanged` for what it changed. Subscriptions are the SDK's
   (`SubscribeHandler`, `UnsubscribeHandler`); the server keeps the set and sends to it.
5. **Tests**, over the in-memory transport as the tool tests are: list, read by full id, by
   prefix, `/meta` as JSON equal to `issue_show`'s structured content, an unknown id refused,
   completion of a prefix, and a subscription that hears an `issue_edit`.
6. **Docs.** The README's MCP section grows the URI table.

## Later, not now

- **A watch on the refs**, so a change made beside the server notifies too. It is the
  filesystem, not the protocol, and belongs to a decision about whether git-issue watches at
  all (`serve` does not either).
- **Attachments as resources**, `issue://<xidr>/files/<path>`, as opaque blobs. Nothing reads
  them today but `serve`; when something does, the URI is there.