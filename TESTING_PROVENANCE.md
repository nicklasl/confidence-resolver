# Testing Provenance Workflows in Your Fork

This guide shows how to test the provenance attestation workflows without making actual releases.

## Setup

1. **Push changes to your fork**:
   ```bash
   git push fork HEAD:provenance
   ```

2. **Go to your fork's Actions tab**:
   `https://github.com/nicklasl/confidence-resolver/actions`

## Manual Testing via workflow_dispatch

The `Release Please` workflow now supports manual testing via the Actions UI.

### Steps:

1. Go to **Actions** → **Release Please** workflow
2. Click **"Run workflow"** button
3. Select checkboxes for the providers you want to test:
   - ☑️ **Simulate Java provider release** - Tests Java JAR build and attestation
   - ☑️ **Simulate JavaScript provider release** - Tests npm package build
   - ☑️ **Simulate Go provider release** - Tests Go provider (and WASM publishing)
   - ☑️ **Simulate Ruby provider release** - Tests Ruby gem build

### What Gets Tested (in Test Mode):

✅ **Always runs (regardless of checkboxes):**
- Docker builds
- WASM artifact extraction
- SHA-256 checksum generation
- **GitHub attestations** (WASM, JAR)

❌ **Skipped in test mode (safety guards):**
- Uploading to GitHub releases
- Publishing to Maven Central
- Publishing to npm
- Publishing to RubyGems

### Example Test Scenarios:

**Test WASM publishing only:**
- Check: ☑️ Simulate Go provider release
- This will trigger `publish-wasm-binary` job
- Builds WASM, creates attestation, but skips upload

**Test Java attestation:**
- Check: ☑️ Simulate Java provider release
- Extracts JAR, creates attestation, shows file info, skips Maven publish

**Test multiple providers:**
- Check: ☑️ Java, ☑️ JavaScript, ☑️ Go
- Tests all three workflows in parallel

## What to Check in Action Logs:

### For WASM binary (`publish-wasm-binary`):
```
✅ Extract WASM binary from Docker
✅ Generate SHA-256 checksum
✅ Attest WASM binary (creates attestation)
✅ Test mode - Show WASM info (displays hash, skips upload)
```

### For Java (`publish-java-provider-release`):
```
✅ Build and extract JAR with Docker
✅ Attest Java JAR (creates attestation)
✅ Test mode - Show JAR info (skips Maven publish)
```

### For JavaScript (`publish-js-provider-release`):
```
✅ Build and extract package tarball
✅ Test mode - Show npm package info (skips npm publish)
```

## Verifying Attestations Were Created:

After a test run, check the attestation was created (note: test attestations might not persist since artifacts aren't uploaded to releases):

```bash
# This may or may not work for test runs, depending on GitHub's attestation behavior
gh attestation verify confidence_resolver.wasm \
  --repo nicklasl/confidence-resolver
```

For a real test, you could:
1. Temporarily remove the `if: github.event_name != 'workflow_dispatch'` from the upload step
2. Create a test release tag manually
3. Run the workflow to actually upload to that test release

## Clean Up

No cleanup needed! Test mode doesn't publish anything or create releases.

## Pushing to Upstream

Once tested in your fork:
```bash
# Create a PR from your branch to spotify/confidence-resolver
git push fork HEAD:provenance
# Then create PR via GitHub UI
```
