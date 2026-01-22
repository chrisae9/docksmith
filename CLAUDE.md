# Docksmith Development Guide

## Deployment
- `docker compose up -d --build` to deploy the app
- Live at: https://docksmith.ts.chis.dev

## Testing
- **Integration tests**: `test/integration/` folder contains real-world Docker container tests
- **When changing ANY functionality** (new or existing), update integration tests and run them
- Hit the API directly when testing - avoid Playwright unless UI-specific nuances require it
- Playwright uses significant context; prefer API calls for functional testing

## Playwright Screenshots
When using the Playwright MCP server for browser testing:
- **Container path**: `/screenshots/` (use this path in screenshot tools)
- **Host path**: `.playwright-screenshots/` (screenshots appear here)
- Example: `filename: "test.png"` saves to `/screenshots/test.png` in container, visible at `.playwright-screenshots/test.png` on host

## Architecture
The dockerized app runs on a Tailscale network:
- `tailscale` container shares the TS network
- `traefik-ts` container (isolated Traefik instance) depends on `tailscale`
- Both are in the `traefik` stack
- Domain `ts.chis.dev` routes through `traefik-ts`

**Critical**: Restarting or updating `tailscale` or `traefik-ts` will cause docksmith to go offline. These services can be managed but understand the connectivity implications.

## Project Status
This app is not released yet - no need to worry about backward compatibility or deprecation. Just fix things directly.

## Bug References
When user provides a UUID like `1e0b30ef-...`, it's a docksmith update/operation ID from the history page. Check the API (`/api/history`) or UI to see the error details.

## Git Practices
- Do NOT commit CLAUDE.md or .claude/ directory to git (they're in .gitignore)
- Only commit actual code changes

## Context Preservation
When compacting, preserve this CLAUDE.md content in the summary - it is not automatically re-read after compaction.