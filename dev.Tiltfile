# humagen on this machine: API, console with hot reload, and Postgres, so
# conversations survive a restart. See docs/guides/develop.md.
#
#   tilt up -f dev.Tiltfile                     # then open http://127.0.0.1:5173
#   tilt up -f dev.Tiltfile -- --memory         # no Docker, nothing kept
#   tilt up -f dev.Tiltfile -- --spec examples/dev/humagen.yaml   # real models
#   tilt down -f dev.Tiltfile                   # stops everything, keeps the database
#
# `make tilt` runs the first. The Tiltfile next to this one runs humagen on a
# local Kubernetes cluster instead.

load('./deploy/tilt/dev/Tiltfile', 'humagen_dev')

config.define_bool('memory', usage = 'keep state in memory instead of Postgres in Docker')
config.define_bool('mock', usage = 'sign in as a persona from the loopback identity provider')
config.define_bool('no-console', usage = 'run the API alone')
config.define_string_list('spec', usage = 'a spec file or directory; repeatable. Watched')
cfg = config.parse()

specs = cfg.get('spec', [])
humagen_dev(
    specs = specs,
    state = 'memory' if cfg.get('memory', False) else 'postgres',
    auth = 'mock' if cfg.get('mock', False) else 'dev',
    console = not cfg.get('no-console', False),
    # Keys for examples/dev/humagen.yaml, from a file git ignores.
    env_file = '.env.dev' if os.path.exists('.env.dev') else None,
    credentials = ['ANTHROPIC_API_KEY', 'OPENAI_API_KEY', 'GOOGLE_API_KEY'] if specs else [],
)
