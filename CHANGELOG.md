# Changelog


## CHORE
- add emit trace logging
- remove debug logs
- bump actions/upload-artifact from 4 to 7
- bump actions/download-artifact from 4 to 8
- bump rollup
- bump minimatch
- bump frontend lint dependencies
- align node version declarations
- bump modernc.org/sqlite from 1.46.1 to 1.51.0
- bump docker/build-push-action from 6 to 7
- bump docker/setup-buildx-action from 3 to 4
- bump docker/login-action from 3 to 4
- bump docker/setup-qemu-action from 3 to 4
- bump softprops/action-gh-release from 2 to 3
- bump typescript-eslint from 8.56.1 to 8.57.1 in /web
- bump github.com/moby/moby/api from 1.53.0 to 1.54.2
- update go dependencies
- update web dependencies
- bump actions/setup-node from 6 to 7
- bump github.com/moby/moby/client from 0.4.1 to 0.5.0
- bump @types/node from 25.9.1 to 25.9.2 in /web
- bump typescript-eslint from 8.60.1 to 8.61.0 in /web
- bump prettier from 3.8.3 to 3.8.4 in /web
- bump actions/dependency-review-action from 4 to 5
- bump actions/setup-go from 6 to 7
- bump actions/checkout from 6 to 7
- bump sharp
- bump prettier from 3.8.4 to 3.9.6 in /web
- bump react-window from 2.2.7 to 2.3.0 in /web
- bump modernc.org/sqlite from 1.51.0 to 1.54.0
- bump react-dom from 19.2.7 to 19.2.8 in /web
- bump github.com/moby/moby/api from 1.55.0 to 1.56.1
- bump modernc.org/sqlite from 1.54.0 to 1.60.1
- bump github.com/moby/moby/client from 0.5.0 to 0.6.2 (#83)
- bump typescript-eslint from 8.61.0 to 8.65.0 in /web (#74)
- bump the npm_and_yarn group across 1 directory with 2 updates (#71)
- bump prettier from 3.9.6 to 3.9.10 in /web (#86)
- bump vite from 8.0.16 to 8.3.4 in /web (#87)
- bump eslint-plugin-react-refresh in /web (#89)
- upgrade toolchains and dependencies
- v0.2.4


## FEATURES
- rework container state, events, and alerts (#11)
- add totals and loop-state fixes for events and alerts
- track broken-since timestamps for container state
- record container rename events
- virtual feeds scroll across total item count
- warn when no-new-privileges is disabled
- persist container memory reservation and limit
- show runtime memory reservation and limit warnings
- expose task finished_at and exit_code on containers
- render task last-run status and metadata
- persist service identity metadata


## FIXES
- repair mismatched event associations
- stabilize container ids on upsert
- show total counts and surface broken containers first
- preserve restart-loop state across start events
- heal persisted restart loops after quiet window
- show bad-since and tighten events feed layout
- keep restart-loop count increasing while loop is active
- place went-bad time under started
- persist per-loop restart counts in alerts
- show restart-loop count from alert details
- rename no-restart failure labels
- reset virtual row heights when top items prepend
- preserve unresolved restart-loop state across recreate/rename
- auto-heal stale restart loop on startup sync
- hide health block when healthcheck is disabled
- only show read-only when disabled
- remove extra mobile right gap in container header
- shorten memory limits runtime label
- restore folded task timing details on right
- avoid duplicate restart loop alerts
- track restart loops by container id
- resolve current container names in history
- normalize historical container names on migrate
- prefer labeled service names for events
- simplify rename event labels in ui
- normalize historical service names


## REFACTOR
- drop last-event and severity from payloads
- derive event and alert severity from types
- remove folded task runtime summary fields
- use compose service identity for monitor state


## CI
- publish dev images on dev branch
- limit dev images to amd64
- auto-merge green Dependabot PRs
- group minor/patch updates, ignore unsupported majors


## STYLE
- format container time lines in App
- format monitor after identity fix

