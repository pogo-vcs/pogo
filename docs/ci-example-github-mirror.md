# CI Example: Mirror to GitHub

This example shows how to automatically mirror your Pogo repository to GitHub whenever you update the `main` bookmark.

## Prerequisites

1. A GitHub repository where you want to mirror your code
2. A GitHub Personal Access Token with `repo` permissions
   - Go to GitHub Settings → Developer settings → Personal access tokens → Tokens (classic)
   - Generate a new token with `repo` scope
3. The token stored as a secret in your Pogo repository

## Setup

### 1. Add the GitHub token as a secret

```bash
pogo secrets set GITHUB_TOKEN ghp_your_token_here
```

### 2. Create the CI configuration

Create a file `.pogo/ci/mirror-to-github.yaml` in your repository:

```yaml
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - type: container
    container:
      image: alpine/git:latest
      environment:
        GITHUB_TOKEN: {{ secret "GITHUB_TOKEN" }}
        GITHUB_REPO: your-username/your-repo
        ARCHIVE_URL: {{ .ArchiveUrl }}
      commands:
        - sh
        - -c
        - |
          set -e
          
          echo "Cloning GitHub repository..."
          git clone https://x-access-token:${GITHUB_TOKEN}@github.com/${GITHUB_REPO}.git /workspace/github
          cd /workspace/github
          
          echo "Configuring Git..."
          git config user.name "Pogo Mirror Bot"
          git config user.email "bot@pogo-mirror"
          
          echo "Clearing workspace (keeping .git)..."
          find . -maxdepth 1 ! -name '.git' ! -name '.' ! -name '..' -exec rm -rf {} +
          
          echo "Downloading Pogo archive..."
          wget -O /tmp/archive.zip "${ARCHIVE_URL}"
          
          echo "Extracting archive..."
          unzip -q /tmp/archive.zip -d /workspace/github
          rm /tmp/archive.zip
          
          echo "Committing changes..."
          git add -A
          
          if git diff --staged --quiet; then
            echo "No changes to commit"
          else
            git commit -m "Mirror from Pogo - {{ .Rev }}"
            
            echo "Pushing to GitHub..."
            git push origin main
            
            echo "Successfully mirrored to GitHub!"
          fi
```

### 3. Commit and push

```bash
pogo describe "Add GitHub mirroring CI job"
pogo push
pogo bookmark set main
```

## How it works

1. **Trigger**: The CI runs when the `main` bookmark is updated
2. **Authentication**: Uses the `GITHUB_TOKEN` secret via the `{{ secret "GITHUB_TOKEN" }}` template function
3. **Clone**: Clones your existing GitHub repository
4. **Clear**: Removes all files except `.git` to ensure a clean mirror
5. **Download**: Downloads the Pogo repository archive using `{{ .ArchiveUrl }}`
6. **Extract**: Extracts the archive contents
7. **Commit**: Creates a commit if there are changes
8. **Push**: Pushes changes to GitHub

## Configuration Variables

Update these in the YAML file:

- `GITHUB_REPO`: Your GitHub repository in format `username/repository-name`
- The `main` branch name in the push command if your GitHub repo uses a different default branch

## Security Notes

- The `GITHUB_TOKEN` is never exposed in logs or output
- Secrets are only accessible to users with repository access
- The token is passed as an environment variable to the container
- Use a token with minimal required permissions (only `repo` scope)

## Customization

### Mirror specific files only

Add a cleanup step before `git add -A`:

```yaml
          echo "Removing unnecessary files..."
          rm -rf tests/
          rm -rf .pogo/
```

### Add a custom commit message

Replace the commit line with:

```yaml
            git commit -m "Sync from Pogo: $(date -u +%Y-%m-%d\ %H:%M:%S\ UTC)"
```

### Mirror to multiple branches

Modify the push command:

```yaml
            git push origin main:main
            git push origin main:production
```

### Skip CI on GitHub

Add `[skip ci]` to avoid triggering GitHub Actions:

```yaml
            git commit -m "Mirror from Pogo - {{ .Rev }} [skip ci]"
```

## Troubleshooting

### Authentication fails

- Verify your token has `repo` permissions
- Check the token hasn't expired
- Ensure the repository name is correct

### Archive download fails

- Check that the Pogo server's `PUBLIC_ADDRESS` is correctly configured
- For private repositories, ensure the archive URL includes authentication

### Git push fails

- Verify the GitHub repository exists
- Check that your token has write access
- Ensure you're not pushing to a protected branch without proper permissions

## Alternative: Using SSH keys

If you prefer SSH over HTTPS, you can mount an SSH key:

```yaml
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - type: container
    container:
      image: alpine/git:latest
      environment:
        SSH_KEY: {{ secret "GITHUB_SSH_KEY" }}
        GITHUB_REPO: git@github.com:your-username/your-repo.git
        ARCHIVE_URL: {{ .ArchiveUrl }}
      commands:
        - sh
        - -c
        - |
          set -e
          
          echo "Setting up SSH..."
          mkdir -p ~/.ssh
          echo "${SSH_KEY}" > ~/.ssh/id_rsa
          chmod 600 ~/.ssh/id_rsa
          ssh-keyscan github.com >> ~/.ssh/known_hosts
          
          echo "Cloning repository..."
          git clone ${GITHUB_REPO} /workspace/github
          
          # ... rest of the script
```
