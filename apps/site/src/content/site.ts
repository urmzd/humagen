// Every fact the site states lives here, so the pages, their metadata and
// llms.txt say the same thing. Inline code is written in backticks and
// rendered by src/lib/inline.ts. The repository is the source of truth:
// README.md, docs/guides/deploy.md, docs/reference/spec.md and the status
// table in docs/architecture/overview.md.

export const site = {
  name: "Humagen",
  url: "https://withhumagen.com",
  repo: "https://github.com/urmzd/humagen",
  license: "Apache-2.0",
  tagline: "The control plane for your team's agents.",
  summary:
    "Humagen is a self-hosted workspace where your team's people and agents work together, on any model, in your cloud, under your rules.",
};

/** A link into the repository's docs on GitHub. */
export const doc = (path: string) => `${site.repo}/blob/main/${path}`;

export type Page = { path: string; title: string; description: string };

export const pages = {
  home: {
    path: "/",
    title: "Humagen: the control plane for your team's agents",
    description:
      "Humagen is the self-hosted control plane for your team's agents: many agents on any model, working beside your people, in your cloud, under your rules.",
  },
  deploy: {
    path: "/deploy",
    title: "Deploy your own Humagen",
    description:
      "The recommended setup: one private Google Cloud VM per team, reached only over Tailscale, with Gemini on Vertex AI and Terraform applied by GitHub Actions.",
  },
} satisfies Record<string, Page>;

export const contact = {
  label: "Talk to the Founder",
  email: "founder@withhumagen.com",
  href: "mailto:founder@withhumagen.com",
};

/** Button labels are Title Case, as in the console. */
export const actions = {
  deploy: "Deploy Your Own",
  github: "View on GitHub",
  guide: "Read the Deploy Guide",
};

export const hero = {
  title: "The control plane for your team's agents",
  lede: "Run agents on the models you choose, next to the people they work with, on machines you own. One workspace, one set of rules and one record of every run.",
  status: "Open source under Apache 2.0.",
};

/** Real console screens, captured from a local preview with seeded example data. */
export const screens = {
  home: {
    alt: "The Humagen console home: a message box that takes an @agent, a Needs you list of items waiting on a person, and recent project posts.",
    caption: "The console, running locally with example data. No model calls.",
  },
  turn: {
    alt: "An agent's answer in a conversation: a recommendation, a decision, a published slide file, one cited knowledge source and four completed work steps.",
    caption: "An answer cites its source, keeps its files and shows its steps.",
  },
  agents: {
    alt: "The Agents page: each agent with its state, revision, model and runs, success rate and median time over 24 hours.",
    caption: "Each agent with its revision, model and the last day's runs.",
  },
};

export type Point = { icon: string; title: string; body: string };

/** The selling points, in the order the home page tells them. */
export const points = {
  together: {
    title: "Agents work together, and with people",
    body: "Tag an `@agent`, or let every available agent decide whether to join. Agents call other agents as subagents, handoffs or delegations. A run waits for a person when it needs an approval or an answer.",
  },
  operate: {
    title: "Each agent has a history",
    body: "Agents have revisions, evals, schedules, alerts and analytics. A run keeps the exact definition it started with, and fails rather than run a prompt that changed.",
  },
  grid: [
    {
      icon: "server",
      title: "Self-hosted",
      body: "One process and Postgres, on a laptop, a VM or Kubernetes. Your data stays in your cloud. The code is open source.",
    },
    {
      icon: "layers",
      title: "Any model, per agent",
      body: "Anthropic, Google Gemini directly or through Vertex AI, or any OpenAI-compatible server, such as Ollama or vLLM. Change an agent's model without moving the workspace.",
    },
    {
      icon: "shield",
      title: "Rules you set",
      body: "A policy file grants roles per project. Nothing is allowed until it is granted. Limits are checked before a run starts, and spend is counted per run.",
    },
    {
      icon: "scroll",
      title: "A record of every run",
      body: "Run traces, the journal and alerts show who asked, which agent answered, what it called and what it cost.",
    },
    {
      icon: "message",
      title: "Where people work",
      body: "The web console, a Slack app in channels and threads, and GitHub comments. Each one shows the same events.",
    },
    {
      icon: "database",
      title: "Answers from your knowledge",
      body: "Knowledge sources per project, with hybrid search over vectors and keywords. Search returns only what the person asking may read.",
    },
    {
      icon: "history",
      title: "Runs that survive a crash",
      body: "Runs are durable and replay after a crash. The ledger decides when a run is over.",
    },
    {
      icon: "wrench",
      title: "Tools that act as you",
      body: "MCP tools run in the sandbox you choose, with each person's own credential, never a shared one.",
    },
  ] satisfies Point[],
};

/** One example roster: three agents on three providers, from the spec reference. */
export const roster = {
  caption: "Example: each agent names its own model.",
  agents: [
    { name: "assistant", hue: 0, model: "Google Gemini through Vertex AI" },
    { name: "reviewer", hue: 210, model: "Anthropic" },
    { name: "local-helper", hue: 120, model: "OpenAI-compatible, on Ollama" },
  ],
};

export const why = {
  heading: "Why Humagen",
  intro: [
    "Hosted assistants such as Claude in Slack, OpenAI Dots and Grok Bot put one vendor's model in your chat. They fit when one model and one vendor's cloud are enough.",
    "Humagen is for teams that need more: several agents, more than one model, and rules that belong to them. Your models, your cloud, your rules.",
  ],
  columns: ["", "Humagen", "A hosted assistant"],
  rows: [
    ["Models", "Any provider you configure, per agent", "The vendor's models"],
    ["Where it runs", "Your laptop, VM or Kubernetes cluster", "The vendor's cloud"],
    [
      "Who sets the rules",
      "Your policy file: roles, projects, limits",
      "The vendor's admin settings",
    ],
    ["Agents", "Many, calling each other and asking people", "One assistant"],
    ["Where people use it", "Web console, Slack and GitHub", "The app it ships for"],
    ["Setup", "You deploy and run it", "Install it and start"],
  ],
};

export const selfHost = {
  heading: "Self-host it, private by default",
  intro:
    "The recommended setup is one private VM per team on Google Cloud. Nobody on the internet can reach it, and there is no model key to leak.",
  points: [
    {
      title: "Only on your tailnet",
      body: "No public IP. Caddy checks each person with Tailscale and passes their login to Humagen. No passwords and no identity provider to run.",
    },
    {
      title: "No API keys",
      body: "Gemini and embeddings go through Vertex AI with the VM's service account. Calls are billed to your project.",
    },
    {
      title: "Updates without SSH",
      body: "Terraform describes it and GitHub Actions applies it. The VM picks up each change on its own. About $25 a month for the VM, plus model calls.",
    },
  ],
};

// ---------------------------------------------------------------------------
// The deploy guide
// ---------------------------------------------------------------------------

export const deployIntro = [
  "One private VM per team on Google Cloud, reachable only over Tailscale. Docker Compose runs Humagen, Postgres and Caddy. Gemini answers through Vertex AI with the VM's service account, so there is no model key to keep. Terraform describes all of it and GitHub Actions applies it.",
  "This is the setup Humagen's author runs in production. The steps show the parts that matter; adapt names, regions and sizes to your project.",
];

export const prerequisites = [
  "A Google Cloud project with billing, and the Compute Engine, Vertex AI, Artifact Registry, Secret Manager and Cloud DNS APIs turned on.",
  "A Tailscale tailnet, and a reusable, tagged auth key stored in Secret Manager. It is added by hand, so it never enters Terraform state.",
  "A domain, or a subdomain you can delegate to a Cloud DNS zone, for the console's address and its certificate.",
  "A GitHub repository for the Terraform, with Actions turned on.",
  "Terraform and `gcloud` on your machine, once, to create the state bucket and CI's identity.",
];

export const sizing =
  "An `e2-medium` (2 vCPU, 4 GB) with 4 GB of swap is enough. In the reference setup the VM and disk cost about $25 a month, plus Vertex AI calls.";

export const architecture = {
  /** The connectors between zones, as the diagram draws them. */
  links: {
    tailnet: "HTTPS on the tailnet",
    outbound: "Outbound only",
    deploys: "Deploys the VM",
  },
  caption:
    "People on the tailnet reach Caddy on the VM. Caddy checks who they are and proxies to Humagen on loopback. The VM has no public address: everything it calls goes out through Cloud NAT.",
  zones: [
    {
      name: "Your tailnet",
      items: [
        "People's laptops and phones",
        "`https://humagen.example.com` resolves to the VM's tailnet address",
      ],
    },
    {
      name: "One GCE VM, no public IP",
      items: [
        "Caddy with caddy-tailscale: certificate by DNS-01, caller identity from tailscaled",
        "Humagen on `127.0.0.1:8080`",
        "Postgres 17 on `127.0.0.1:5432`",
        "`humagen-watch`: re-runs the startup script when Terraform changes it",
      ],
    },
    {
      name: "Outbound through Cloud NAT",
      items: [
        "Vertex AI: Gemini and embeddings, with the VM's service account",
        "Artifact Registry: the Humagen and Caddy images",
        "Secret Manager: the Tailscale auth key",
        "Cloud DNS: the tailnet address and the ACME challenge",
      ],
    },
    {
      name: "GitHub Actions",
      items: [
        "Terraform plan on pull requests, apply on main, through Workload Identity Federation",
        "Builds the images and pushes them to Artifact Registry",
      ],
    },
  ],
};

export type Step = {
  id: string;
  title: string;
  body: string[];
  code?: { label: string; lang: string; text: string };
  after?: string[];
};

const spec = `api_version: humagen/v1
kind: System
metadata:
  name: humagen
spec:
  server:
    public_url: https://humagen.example.com
  auth:
    kind: tailscale            # Caddy passes the caller's login from loopback
    tenant: {fixed: default}
    policy_file: /etc/warp/policy.authz
  stores:
    kind: postgres
    postgres: {url: {ref: env:HUMAGEN_POSTGRES_URL}}
  bus:
    kind: postgres
  blobs:
    kind: disk
  knowledge:
    embedder:
      kind: vertex
      vertex: {project: YOUR_PROJECT, location: us-central1}
  sandbox:
    kind: unisolated           # no Docker socket in the container
  execution:
    builtin: {provider: gemini, model: gemini-3-flash-preview}
---
api_version: humagen/v1
kind: Platform
metadata:
  name: default
spec:
  models:
    - name: gemini
      kind: google
      google:
        vertex: {project: YOUR_PROJECT, location: global}
  agents:
    - spec:
        name: assistant
        provider: gemini
        model: gemini-3-flash-preview
        system_prompt: You are a helpful assistant. Answer plainly.`;

const compose = `services:
  humagen:
    image: REGION-docker.pkg.dev/YOUR_PROJECT/humagen/humagen:v0.46.0
    network_mode: host
    command: serve -f /etc/warp -addr 127.0.0.1:8080 -web /usr/share/warp/web
    env_file: .env             # HUMAGEN_POSTGRES_URL, made on the VM
    volumes:
      - humagen-data:/var/lib/warp
      - ./config:/etc/warp:ro
    depends_on:
      postgres: {condition: service_healthy}

  postgres:
    image: postgres:17-alpine
    ports: ["127.0.0.1:5432:5432"]
    volumes: [postgres-data:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U humagen -d humagen"]

  caddy:
    image: REGION-docker.pkg.dev/YOUR_PROJECT/humagen/caddy:TAG
    network_mode: host
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - /var/run/tailscale:/var/run/tailscale`;

const caddyfile = `humagen.example.com {
	tls {
		dns googleclouddns {
			gcp_project YOUR_PROJECT
		}
	}

	tailscale_auth

	reverse_proxy 127.0.0.1:8080 {
		header_up -Tailscale-User-Groups
		header_up -Tailscale-User-Profile-Pic
		header_up Tailscale-User-Login {http.auth.user.tailscale_user}
		header_up Tailscale-User-Name {http.auth.user.tailscale_name}
	}
}`;

const policy = `role admin { allow * on * }
grant admin to subject "tailscale:you@github"`;

const verify = `# From a laptop on the tailnet
curl -fsS https://humagen.example.com/healthz     # ok

# On the VM, over IAP SSH
gcloud compute ssh humagen --zone ZONE --tunnel-through-iap -- \\
  'sudo docker compose -f /opt/humagen/compose.yaml ps; curl -fsS http://127.0.0.1:8080/healthz'

# Follow a rollout
gcloud compute ssh humagen --zone ZONE --tunnel-through-iap -- \\
  sudo journalctl -u humagen-watch -f`;

export const steps: Step[] = [
  {
    id: "network",
    title: "A private network and VM",
    body: [
      "Create a VPC whose firewall admits nothing but IAP SSH (`35.235.240.0/20` on port 22). Tailscale connects outbound, so it needs no inbound rule. Add a Cloud Router with Cloud NAT so the VM can reach Google APIs, Tailscale and the package mirrors.",
      "Give the VM no access config, so it has no public address, and its own service account with only what it uses: `roles/aiplatform.user` for Vertex AI, `roles/logging.logWriter`, `roles/artifactregistry.reader` on the image repository, access to the Tailscale key's secret, and `roles/dns.admin` on its own zone.",
      "The startup script installs Docker and Tailscale, joins the tailnet once with the key from Secret Manager, writes the VM's tailnet address into the zone's A record, makes a database password that never leaves the VM, writes the files below and starts the stack. The name resolves for anyone, but only tailnet members can reach the address.",
    ],
  },
  {
    id: "compose",
    title: "Humagen, Postgres and Caddy in Compose",
    body: [
      "Everything is on the host network and bound to loopback, except Caddy. Humagen must listen on loopback: Tailscale sign-in refuses any other address.",
    ],
    code: { label: "compose.yaml", lang: "yaml", text: compose },
    after: [
      "The image paths keep their original names (`/etc/warp`, `/var/lib/warp`) so older volumes and specs keep working.",
    ],
  },
  {
    id: "spec",
    title: "The spec",
    body: [
      "One file holds two documents. The `System` document says how Humagen runs: sign-in, stores, files, knowledge, sandbox. The `Platform` document says what the workspace offers: models and agents. Secrets are references such as `env:HUMAGEN_POSTGRES_URL`, never values.",
    ],
    code: { label: "config/humagen.yaml", lang: "yaml", text: spec },
    after: [
      "Run `humagen config check -f config/humagen.yaml` before you ship a change. It does not resolve references or read the files a spec names, so an unset variable shows only at start.",
    ],
  },
  {
    id: "sign-in",
    title: "Sign in with Tailscale",
    body: [
      "With `auth.kind: tailscale` there is no identity provider and no password. Caddy's `tailscale_auth` asks the local tailscaled who each caller is and refuses anyone it does not know, including the internet and tagged devices. It replaces the identity headers a caller sent, and Humagen trusts them only from loopback.",
    ],
    code: { label: "Caddyfile", lang: "caddy", text: caddyfile },
    after: [
      "Caddy gets its certificate from Let's Encrypt with a DNS-01 challenge on Cloud DNS, so the VM never needs to be reachable from the internet. Build it with `xcaddy` and the `caddy-tailscale` and `caddy-dns/googleclouddns` modules.",
      "Every tailnet member signs in with no permissions until the policy grants one. Grant roles to Tailscale logins:",
    ],
  },
  {
    id: "models",
    title: "Models on Vertex AI",
    body: [
      "A model of `kind: google` with a `vertex` block calls Gemini through Vertex AI with Application Default Credentials: on the VM, its service account. There is no API key to store or rotate, and calls are billed to the project.",
      "Gemini 3 previews are served from the `global` location. Agents can still name other providers in the same `Platform` document, with their keys as `env:` references.",
    ],
  },
  {
    id: "knowledge",
    title: "Knowledge with hybrid search",
    body: [
      "`knowledge.embedder: vertex` gives each passage a Vertex AI embedding (`gemini-embedding-001`, 768 dimensions by default) in the background, so an upload never waits on a model. Search ranks passages by vector similarity and by BM25 and fuses the two lists.",
      "If the embedder fails, writes still succeed and search falls back to lexical. Search sees exactly the documents it would without vectors: the same sources and access checks.",
    ],
  },
  {
    id: "ci",
    title: "CI applies Terraform and builds the images",
    body: [
      "GitHub Actions reaches Google Cloud through Workload Identity Federation, with no service account keys. Pull requests run `terraform plan` as a read-only account and post the plan as a comment. A merge to main runs `terraform apply` in a protected `production` environment.",
      "An images job builds each tag missing from the private Artifact Registry: Humagen from `urmzd/humagen` at the release you pin (about 10 minutes), and the custom Caddy. Releases do not publish an image yet, so this build is required.",
      "On the VM, `humagen-watch`, a systemd service, waits on the instance's `startup-script` metadata. When an apply changes it, the service re-runs the script, which writes the new config, retries `docker compose pull` for up to 30 minutes until the new images exist, and restarts the stack. Upgrading is a one-line change to the pinned release. No SSH needed.",
    ],
  },
  {
    id: "verify",
    title: "Check that it works",
    body: [
      "`/healthz` answers `ok`. Open the console at your domain from a tailnet device: you are signed in as your Tailscale login.",
    ],
    code: { label: "Shell", lang: "sh", text: verify },
    after: [
      "The first boot waits for CI's images, about 10 to 15 minutes. Its log is `/var/log/humagen-startup.log` on the VM.",
    ],
  },
];

export const policySnippet = { label: "config/policy.authz", lang: "text", text: policy };

export const alternatives = {
  heading: "Other ways to run it",
  intro: "Every setup runs the same process with the same spec.",
  rows: [
    {
      name: "`humagen start`",
      when: "Try it on your laptop",
      what: "One process: loopback sign-in, in-memory stores, seeded example agents on an offline model. No key needed.",
      link: { label: "Quick start in the README", href: `${site.repo}#quick-start` },
    },
    {
      name: "Docker Compose with OIDC",
      when: "One host, your identity provider",
      what: "Humagen and Postgres 17, with optional MinIO for files. Sign-in in a container is OIDC.",
      link: {
        label: "Docker Compose guide",
        href: `${doc("docs/guides/deploy.md")}#docker-compose`,
      },
    },
    {
      name: "Helm on Kubernetes",
      when: "A cluster you already run",
      what: "One replica, non-root, read-only root filesystem. Bring your own Postgres. Flux can deploy it from Git.",
      link: {
        label: "Helm chart guide",
        href: `${doc("docs/guides/deploy.md")}#kubernetes-with-helm`,
      },
    },
    {
      name: "`humagen deploy`",
      when: "Let the spec choose",
      what: "The spec's `deploy` slot picks `local`, `compose` or `kubernetes`. `-o DIR` writes what it would apply and stops.",
      link: {
        label: "Deploying it, in the spec reference",
        href: `${doc("docs/reference/spec.md")}#deploying-it`,
      },
    },
  ],
};

export const limits = {
  heading: "Know before you deploy",
  intro: "What this setup does not do yet, from the status table in the repository.",
  items: [
    "**One replica.** The stores are single-writer, and a second process on the same database is refused at start.",
    "**Sessions are in memory.** A restart signs everyone out.",
    "**No published image.** Build it from the repository and push it to your registry, as the CI above does.",
    "**Tools in this setup are not isolated.** `sandbox.kind: unisolated` runs file tools and stdio MCP servers in Humagen's own container and refuses the shell. The `docker` driver isolates each run but needs a dedicated sandbox host, and its instances have no network yet. A Kubernetes sandbox driver is not built.",
    "**Tailscale sign-in has no Slack app.** A process cannot sign in through Tailscale, so the Slack app needs OIDC sign-in. Slack and GitHub are tested against simulated APIs.",
  ],
  link: {
    label: "Status table in the architecture overview",
    href: `${doc("docs/architecture/overview.md")}#status`,
  },
};
