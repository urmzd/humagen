# Authorization

Sign-in establishes who a caller is. A policy decides what they may do. The
policy maps verified claims to roles, and roles to permissions, and every
`Principal.Can` is answered by evaluating it. The language lives in
`pkg/authz`; `pkg/auth` supplies the permissions and applies it.

## Grammar

```ebnf
policy    = { statement } ;
statement = role | grant ;
role      = "role" IDENT [ "extends" IDENT { "," IDENT } ] "{" { rule } "}" ;
rule      = ( "allow" | "deny" ) actions "on" resources ;
actions   = "*" | IDENT { "," IDENT } ;          (* read, write *)
resources = "*" | IDENT { "," IDENT } ;          (* agents, runs, schedules, ... *)
grant     = "grant" IDENT { "," IDENT } "to" subject ;
subject   = "group" STRING
          | "subject" STRING
          | "email" "domain" STRING
          | "claim" ( IDENT | STRING ) "=" STRING
          | "anyone" ;

IDENT     = letter_or_underscore { letter | digit | "_" | "-" } ;
STRING    = '"' { any character but '"', "\" or a newline | '\"' | "\\" } '"' ;
```

`#` starts a comment to the end of the line. The words of the language are
reserved and cannot name a role, action or resource. A claim's name can be any
word, or a string for a name such as `"https://example.com/roles"`.

## Semantics

- **Default deny.** A permission is held only if a rule allows it.
- **Deny wins.** A deny beats every allow, wherever either comes from: the same
  role, a role it extends, a role that extends it, or another role held.
- **Inheritance.** A role holds its own rules and every rule of the roles it
  extends, transitively.
- **Grants.** A person holds every role of every grant whose subject matches
  their verified claims. `group` matches one entry of the groups claim.
  `subject` matches `sub`. `email domain` matches the part after `@`, ignoring
  case, and only when `email_verified` is `true`. `claim` matches a claim
  whose value is exactly that string; a missing claim, a list, a number or a
  boolean never matches. `anyone` matches any person with a subject and a
  tenant, never the zero `Principal`.
- **Machines are not decided by the policy.** A machine gets exactly the scopes
  on its token. It holds no roles, so no grant, `anyone` included, widens it.
- **Errors at load time.** A syntax error, an unknown role in `extends` or a
  grant, a cycle, a role declared twice, an unknown action or resource, or a
  policy with no roles stops the server from starting. Actions and resources
  are those of `auth.Permissions()`. A policy is limited to 256 roles, 4096
  rules, 4096 grants and 32 levels of inheritance.

## Example

```
role viewer   { allow read on * }
role operator extends viewer {
  allow write on runs, schedules, alerts, evals, artifacts, projects
}
role admin    { allow * on * }
role auditor  extends viewer { deny read on billing }
role steward  extends viewer { allow govern on projects }

grant viewer   to anyone
grant operator to group "eng-oncall"
grant admin    to group "platform-admins"
grant auditor  to claim department = "finance"
```

Someone in `platform-admins` whose `department` is `finance` holds viewer,
admin and auditor, and cannot read billing: the auditor's deny wins.

Projects have three permissions: `projects:read`, `projects:write`, and
`projects:govern`, the action `govern`, which changes the workspace itself and
company projects and reuses a deleted project's name. `allow * on *` holds it,
and so does a rule that names it, as `steward` does above. It adds to
`projects:write` and never replaces it: a steward who cannot write projects
changes none. A policy file written before projects had their own permissions
adds `projects` to the rule that lets operators write runs, as above, or only
admins make projects.

## Project roles

A project role narrows what a permission allows; it never grants one. Every
project route checks one tenant permission, then the caller's role on the
project it names: the action is allowed when the caller holds the permission
and their role is at least the one it needs. A project the caller has no role
in answers 404, as a missing one does, and a visible one with too low a role
answers 403, saying which role it needs ("you need to own Globex").

Roles are `viewer < member < owner`, granted by a project's members: `*`
(everyone who can see the parent; at the workspace, everyone holding
`projects:read`), `role:<name>` (the holders of a policy role, so IdP groups
reach a project through a `grant`), or a subject. `Resolve` reads a role
along the chain, the workspace first: at each level the best entry that
matches, an owner above owns every level below, and no role at one level
means none below it. `projects:govern` is owner everywhere. A machine matches
only `*` and its own subject, never a `role:` entry.

```
role field-eng {}
grant operator, field-eng to group "field-engineering"
```

and then `{who: "role:field-eng", role: member}` on the project.

| Action | Permission | Role |
|---|---|---|
| See a project, its inbox, history, members and notes | `projects:read` | viewer |
| Create a project under it | `projects:write` | member (company kind: `projects:govern`) |
| Propose a note, which its owners keep or discard | `projects:write` | member |
| Change it: title, description, instructions, account, kind, archiving, members, the agents it turns on, who may announce; add, keep or remove notes; delete it | `projects:write` | owner (the workspace and company projects: `projects:govern` too) |
| Make an agent live in it, or move one out | `agents:write` | owner of the project it comes to and of the one it leaves |
| Edit an agent that lives in it | `agents:write` | owner |
| Move it | `projects:write` | owner, and member of the new parent; leaving a project that holds a limit needs `billing:write` |
| Set a tighten-only limit on it | `projects:write` | owner of a project above it |
| Post an announcement | `projects:write` | the project's `announce` role, owner by default |
| Withdraw an announcement | `projects:write` | its author, or owner |
| Hide an announcement for oneself | `projects:read` | |

Until reads are confined to members, every project keeps an entry for `*` of
at least viewer, so a role decides what a person may change, not what they
may read. A project record from before roles reads as everyone owning it,
which is what any operator could do to it; the workspace reads as everyone a
member, owned by `projects:govern`.

The directory has one permission, `people:read`: seeing who is in the
workspace, by name, handle, picture and roles. `allow read on *` holds it, so
every built-in role does, and a policy file that lists reads by resource adds
`people`. A grant to a subject (`grant operator to subject "okta|00u1ana"`)
also puts that person in the directory before they first sign in.

Setting the workspace default agent, the root's `default`, is a change to the
workspace itself, so it needs `projects:govern` as well as `projects:write`.
Managers held it through `agents:write` while it was a setting called the
baseline; `role steward` above, or `allow govern on projects` on `manager`,
gives it back to them.

## Configure

```sh
export WARP_AUTHZ_POLICY=/etc/warp/policy.authz
warp serve ...
```

Without it, the built-in policy (`pkg/auth/default.authz`) declares `viewer`,
`operator`, `manager` and `admin` with exactly the permissions in
[security](security.md#roles-and-scopes), and grants nothing itself. The
`-admin-group`, `-manager-group`, `-operator-group` and `-viewer-group` flags
become grants of those roles. With a policy file, the file decides, and a group flag is added
as a grant only if it is set on the command line.

## Debug with Explain

`authz.Policy.Explain(identity, permission)` returns the outcome (`allowed`,
`denied` or `no match`), the rule that decided, the role it reached the caller
through, the grant that gave that role, and every role held.
`auth.Policy.Explain(principal, permission)` does the same for a signed-in
`Principal`. A sign-in keeps the person's roles and, for each, the grant of
the policy that gave it, so the explanation names the grant. It does not keep
the claims or the token. A `Principal` made any other way names the rule and
not the grant.

A signed-in person asks about themselves with
`GET /api/access/explain?permission=agents:write`. It takes a permission and
no subject, so nobody can ask about someone else, and any signed-in caller may
call it. The console shows the answer under Access, reached from "Why can I do
this?" on the home page.

From the command line, with the same environment and flags as `serve`:

```sh
warp authz check policy.authz
WARP_AUTHZ_POLICY=policy.authz \
  warp authz explain -claims id-token-claims.json billing:read
```

```
billing:read: denied
  rule:  deny read on billing (policy.authz:6:32)
  role:  auditor
  grant: grant auditor to claim department = "finance" (policy.authz:11:1)
  a deny overrides every allow
  holds: viewer (anyone), admin (group "platform-admins"), auditor (claim department = "finance")
```

`-claims` is a JSON file of a token's claims, such as a decoded ID token's
payload.

## Sign in locally

`make dev-signin` runs the server with `-auth=mock`: a development identity
provider on `127.0.0.1:8081` with a page of personas and no passwords. Sign-in
goes through the same flow, verifier and policy as a real provider. The
built-in personas are a viewer, an operator, a manager, an admin and someone in no
mapped group, and a machine `mock-ci` (secret `mock-ci-secret`) with
`warp:runs:read` and `warp:runs:write`:

```sh
curl -s -u mock-ci:mock-ci-secret -d grant_type=client_credentials http://127.0.0.1:8081/token
```

`-mock-personas` reads others from a JSON file of
`{"people": [...], "machines": [...]}`, with the fields of `mockidp.Person`
and `mockidp.Machine`. The provider refuses to start unless selected with
`-auth=mock`, refuses any address but `127.0.0.1` or `localhost`, refuses a
redirect that is not loopback, and the server itself must listen on loopback.
