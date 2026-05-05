# GOMI Project Guidelines

milestone is written in docs/MILESTONE.md

## UI Implementation Policy

- Implement VM and Machine UI with shared patterns and components whenever possible.
- When adding a feature to one side, such as badges, form fields, or dialog UI, add equivalent support to the other side.
- Keep styles, including colors, sizing, and layout, consistent between both UI areas.

## Core Concepts for OS Deployment

Target machines are home PCs (x86_64), Raspberry Pi devices, or similar machines.
Target operating systems include Ubuntu, Debian-family distributions, Red Hat-family distributions, and Fedora. The system is expected to support multiple versions, so do not decide that tests or implementations are sufficient just because they work on a single specific version.

## Ignore Backword Compatibility
This project is under development.
It can introduce breaking changes.
