# GOMI Project Guidelines

milestone is written in docs/MILESTONE.md

## UI Implementation Policy

- Implement VM and Machine UI with shared patterns and components whenever possible.
- When adding a feature to one side, such as badges, form fields, or dialog UI, add equivalent support to the other side.
- Keep styles, including colors, sizing, and layout, consistent between both UI areas.

## Core Concepts for OS Deployment

Target machines are home PCs (x86_64), Raspberry Pi devices, or similar machines.
Target operating systems include Ubuntu, Debian-family distributions, Red Hat-family distributions, and Fedora. The system is expected to support multiple versions, so do not decide that tests or implementations are sufficient just because they work on a single specific version.

When changing OS image, rootfs artifact, curtin, cloud-init, PXE deploy, or post-install customization logic, design for multiple OS families from the start. Avoid hardcoding Ubuntu/Debian-specific package managers, paths, cloud-init behavior, bootloader assumptions, or network configuration unless the condition is explicitly selected by catalog metadata, OS family, image format, or another typed capability. If an implementation is intentionally limited to one OS family, keep the limitation isolated behind a clearly named branch or helper and add tests that prove unsupported families fail early with an explicit error rather than silently receiving Ubuntu-specific behavior.

Keep deploy artifact concepts precise. Whole-disk raw artifacts, rootfs SquashFS artifacts, ISO installers, and future filesystem artifacts should be represented by typed format/capability fields rather than inferred from filenames or from a single Ubuntu example. Tests should cover at least one non-Ubuntu path or an explicit unsupported-family error whenever OS deploy behavior is changed.

## Ignore Backword Compatibility
This project is under development.
It can introduce breaking changes.
