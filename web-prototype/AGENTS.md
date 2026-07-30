# Prototype Instructions

Run the local server yourself and open the preview in the browser available to this environment. Do not give the user server-start instructions when you can run it.

Before making substantial visual changes, use the Product Design plugin's `get-context` skill when the visual source is unclear or no longer matches the current goal. When the user gives durable prototype-specific design feedback, preferences, or decisions, record them in `AGENTS.md`.

When implementing from a selected generated mock, treat that image as the source of truth for layout, component anatomy, density, spacing, color, typography, visible content, and hierarchy.

Build app UI in `src/`. Keep `.openai/hosting.json`, `worker/index.js`, `scripts/prepare-sites-build.mjs`, and `tests/sites-worker.test.mjs` intact so the same local prototype can be handed to Sites. Before a Sites handoff, run `npm run build` and `npm run test:sites`; the build must leave `dist/client/index.html`, `dist/server/index.js`, and `dist/.openai/hosting.json`.

The Ubuntu Manager at 192.168.1.99 is a monitoring and configuration surface, not a PVE shutdown controller. Its reverse proxy must use an exact method-and-path allowlist and must never forward Guest shutdown, stop-all, or host-poweroff requests. The Guest view may read status and test QEMU Guest Agent only. Real shutdown decisions and execution stay local to the PVE host; the guarded PVE CLI remains a local recovery interface.

The Web console must have an in-page account login and logout flow; do not rely on an unexplained browser Basic Auth prompt. Keep human Web sessions separate from PVE API or node credentials. Offer system, light, and dark appearance choices, remember the choice locally, and apply it to login, dashboard, drawers, dialogs, and mobile layouts.

WOL controls must use the real Manager API and its server-side device list. Require a confirmation dialog before starting a wake task, show packet attempts rather than claiming the device is online, and never fall back to fake WOL success in production.

UPS load and battery status cards should open real Manager-backed detail views. Load charts must identify the sampled time range; battery views must distinguish rated DC energy, runtime-based estimates, and measured discharge capacity, and replacement advice must list its evidence rather than infer health from charge percentage alone.

Outage timing controls must load from and save to the real PVE API in `production` mode. Dell P7920 details must provide direct timing and simulation entry points. The Manager must never proxy PVE shutdown endpoints; only the local PVE Guard may execute automatic protection. Simulations remain visibly and structurally fixed to DRY-RUN and may display `would_run` commands but must never invoke a PVE write executor.
