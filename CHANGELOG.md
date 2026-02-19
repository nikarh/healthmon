# Changelog


## CHORE
- add docker event capture scripts
- refine docker event scenario
- replace capture scripts with scenario runner
- switch scenarios to toml
- v0.2.0


## DOCS
- document role labels and events endpoint


## FEATURES
- track roles, presence, and global events
- add events feed and sorting
- add logo and mobile header tweaks
- virtualize all-events feed
- dynamic events sizing and padding model
- capture docker inspect snapshots


## FIXES
- avoid per-container last event backfill
- avoid startup hang with existing db
- avoid shared pointers on load
- correct autosizer import and add event separation
- use react-window list export
- stabilize virtual events list
- cap events list to container height
- keep last row background
- allow dev websocket origins
- preserve hijacker for websocket logging
- handle die exits and missing names
- handle stop events and empty exit codes
- drop removed containers in ui


## TESTS
- replay docker events against mock server
- default replay data to scenario dumps
- add captured scenario dumps
- validate replay via ws and http


## STYLE
- restore strong hover shadow without shift
- tighten events feed padding
- restore events padding with tighter scrollbar
- tighten events ids and layout height
- align header title on desktop
- align status dots to first line on mobile
- tweak mobile spacing
- simplify events feed content
- move page padding to content
- boost header contrast
- condense container event rows
- hide empty container events section
- hide empty last-event summary
- omit last-event layout when empty

