# GOMI Project Guidelines

milestone is written in docs/MILESTONE.md

## Implementation Hygiene

- Keep implementation files focused on one responsibility. As a guideline, aim for source files to stay under about 300 lines when practical; if a file approaches 500 lines, first look for a natural split before adding more behavior.
- Prefer small, named helpers over long inline blocks. Functions should normally fit on one screen and have a single clear reason to change.
- Do not hide unrelated refactors inside feature work or hotfixes. If cleanup is needed, keep it local to the touched behavior and make the intent obvious in the diff.
- Keep tests close to the behavior they verify. Add or update focused tests for new branching logic, error handling, OS-family selection, deployment artifact handling, and UI state transitions.
- Generated files, fixtures, vendored code, and intentionally table-driven data may exceed the line-count guideline, but avoid mixing hand-written business logic into those large files.

## UI Implementation Policy

- Implement VM and Machine UI with shared patterns and components whenever possible.
- When adding a feature to one side, such as badges, form fields, or dialog UI, add equivalent support to the other side.
- Keep styles, including colors, sizing, and layout, consistent between both UI areas.

## Core Concepts for OS Deployment

Target machines are home PCs (x86_64), Raspberry Pi devices, or similar machines.
Target operating systems include Ubuntu, Debian-family distributions, Red Hat-family distributions, and Fedora. The system is expected to support multiple versions, so do not decide that tests or implementations are sufficient just because they work on a single specific version.

When changing OS image, rootfs artifact, curtin, cloud-init, PXE deploy, or post-install customization logic, design for multiple OS families from the start. Avoid hardcoding Ubuntu/Debian-specific package managers, paths, cloud-init behavior, bootloader assumptions, or network configuration unless the condition is explicitly selected by catalog metadata, OS family, image format, or another typed capability. If an implementation is intentionally limited to one OS family, keep the limitation isolated behind a clearly named branch or helper and add tests that prove unsupported families fail early with an explicit error rather than silently receiving Ubuntu-specific behavior.

Keep deploy artifact concepts precise. Whole-disk raw artifacts, rootfs SquashFS artifacts, ISO installers, and future filesystem artifacts should be represented by typed format/capability fields rather than inferred from filenames or from a single Ubuntu example. Tests should cover at least one non-Ubuntu path or an explicit unsupported-family error whenever OS deploy behavior is changed.

## Production Debugging and Hotfix Discipline

When debugging or recovering real machines, do not turn a node-specific workaround into a general code change unless its blast radius has been checked. Before committing any change found during live validation, explicitly verify whether it affects other machines, virtual machines, OS families, firmware modes, boot modes, or already-working deployment paths.

For PXE, boot, DHCP, curtin, cloud-init, power control, networking, libvirt, and OS image changes, prove that the change is either safely scoped to the affected target or valid for all affected targets. This proof must include code inspection and tests or live checks that cover representative unaffected paths. If that proof is not available, keep the workaround operational-only and do not commit it.

Spot fixes are acceptable only when the condition is narrowly selected by typed metadata or explicit target identity and when the fallback behavior for other nodes is unchanged. Avoid changing global defaults during live recovery unless you have first confirmed that every caller and deployment scenario depending on that default remains correct.

Before pushing a fix discovered during live validation, document the side-effect check in the commit/PR notes: what paths may be affected, what was verified, and what remains unverified.

When exact e2e verification is required, do not deviate from the regular product or operational procedure. For root-cause investigation after a failed or confusing run, temporary deviations such as direct database inspection or edits are allowed, but treat them as diagnostic actions rather than valid e2e proof.

## Ignore Backword Compatibility
This project is under development.
It can introduce breaking changes.
