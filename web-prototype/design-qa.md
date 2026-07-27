# PowerCheck Web Prototype — Design QA

**Comparison target**

- Source visual truth: `C:\Users\1\Documents\PVE\web-prototype\reference\option-1.png`
- Implementation URL: `http://127.0.0.1:4173/`
- Implementation screenshots:
  - `C:\Users\1\Documents\PVE\web-prototype\qa-desktop.png`
  - `C:\Users\1\Documents\PVE\web-prototype\qa-mobile.png`
  - `C:\Users\1\Documents\PVE\web-prototype\qa-settings.png`
- Combined comparison: `C:\Users\1\Documents\PVE\web-prototype\qa-comparison.png`
- Viewports: desktop `1440 × 1024` CSS px; mobile `390 × 844` CSS px
- Pixels and normalization:
  - Source: `1487 × 1058` px, normalized to `1440 × 1024` for the combined comparison.
  - Desktop implementation: `1440 × 1024` px at `deviceScaleFactor: 1`.
  - Mobile implementation: `390 × 844` px at `deviceScaleFactor: 1`.
- State: dark theme, overview route, DRY-RUN active, normal mains/NUT state, two PVE nodes, default event and WOL data.

**Full-view comparison evidence**

- The combined comparison preserves the source hierarchy: fixed left navigation, top status bar, five-column power summary, PVE node table, event/WOL split panels, and bottom actions.
- Desktop proportions, section order, dark navy palette, green/amber/blue semantic colors, thin dividers, and compact operations-console density are consistent with the source.
- The implementation deliberately uses slightly flatter surfaces and smaller UI text than the rendered concept. This does not alter hierarchy, scanning order, or above-the-fold content and is classified as P3 polish.

**Focused region comparison evidence**

- PVE node table: node identity, online state, Guest count, Agent health, NUT source, and note columns match the source and remain readable.
- Status band: all five source metrics are present with the same order and semantic colors.
- Lower panels: the event timeline and WOL metrics/device list reproduce the source content structure and status encoding.
- Mobile focused capture confirms that the status grid, node cards, and six-item bottom navigation fit without horizontal overflow.
- Settings capture confirms that editable timing fields, PVE targets, local configuration status, validation, and the derived timeline use the established console tokens without clipping.

**Findings**

- No actionable P0, P1, or P2 findings remain.
- [P3] The source uses larger pictograms and a soft textured/elevated treatment; the implementation uses crisp Phosphor icons and flatter CSS surfaces. This keeps the interface sharper and lighter while preserving the intended visual language.
- [P3] The source typography is marginally larger in several dense tables. The implementation favors slightly smaller text so the full dashboard remains visible at `1440 × 1024`.

**Required fidelity surfaces**

- Fonts and typography: local Inter Variable plus Noto Sans SC Variable; weights, wrapping, line height, and hierarchy are stable at desktop and mobile sizes.
- Spacing and layout rhythm: source region proportions, 8 px radii, borders, row rhythm, and section gaps are matched; no desktop or mobile horizontal overflow.
- Colors and visual tokens: navy surfaces, muted blue-gray text, green healthy state, amber DRY-RUN state, and blue action/navigation state map consistently to the source.
- Image quality and asset fidelity: the source has no photography or illustration assets. Visible UI pictograms use one open-source vector icon family; no emoji, placeholder art, CSS drawings, or handcrafted SVG substitutes are used.
- Copy and content: dashboard labels, state text, node data, event text, WOL data, and safety language are coherent and consistent with the selected concept.

**Interaction and runtime evidence**

- Primary interactions tested:
  - DRY-RUN safety modal opens and closes.
  - Drill detail modal opens and closes.
  - PVE node detail drawer opens; inside clicks are preserved; outside click, close button, and `Esc` close it.
  - Settings can edit a draft timing budget, preview the derived emergency time, select PVE targets, and apply the draft without changing active overview values before successful apply.
  - Manual scan enters loading state and resolves to a success notice/event.
  - Desktop and mobile responsive layouts were checked.
- Browser console: checked; no errors or warnings.
- Automated command: `npm run test:ui`
- Build command: `npm run build`
- Packaging tests: `npm run test:sites`

**Comparison history**

1. Initial mobile capture exposed a P2 responsive issue: the desktop node table remained wider than the phone viewport and persistent bottom navigation could be visually clipped.
   - Fix: converted node rows into compact two-column mobile cards; reduced mobile status spacing; changed bottom navigation tracks to `minmax(0, 1fr)`; made the node-details affordance icon-only on small screens.
   - Post-fix evidence: `qa-mobile.png` at `390 × 844`; all six navigation items and both node cards fit without horizontal overflow.
2. Runtime QA exposed a missing favicon request as a browser-console 404.
   - Fix: added an explicit data favicon, Chinese document language, descriptive metadata, and a product title.
   - Post-fix evidence: `npm run test:ui` reports an empty `consoleErrors` array.
3. Final desktop comparison found no remaining P0/P1/P2 mismatch.
   - Evidence: `qa-comparison.png`.
4. The static drill timeline did not expose editable time configuration or make the execution location clear.
   - Fix: added a PVE-local settings drawer with validated timing fields, node targeting, draft/applied separation, derived timeline preview, and explicit `/etc/powercheck/config.yaml` status.
   - Post-fix evidence: `qa-settings.png` and the `editable timing configuration and local PVE apply feedback` browser test.

**Open Questions**

- None for this visual prototype. Real PVE/NUT/WOL data binding and authenticated settings are intentionally outside this design-only stage.

**Implementation Checklist**

- [x] Match selected desktop concept and information hierarchy.
- [x] Preserve DRY-RUN safety language.
- [x] Implement navigation, node drawer, safety/drill modals, and scan feedback.
- [x] Verify desktop and mobile layout.
- [x] Verify build, worker packaging tests, and browser console.

**Follow-up Polish**

- If desired, slightly increase desktop icon scale and add a restrained surface glow to move closer to the rendered concept.

final result: passed
