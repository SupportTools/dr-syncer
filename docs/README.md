# DR-Syncer Documentation

This directory contains the Docusaurus-based documentation site for DR-Syncer.

## Setup for Custom Domain (dr-syncer.io)

The documentation is configured to be published to GitHub Pages with the custom domain `dr-syncer.io`. To complete the setup process:

### 1. GitHub Repository Settings

1. Go to the GitHub repository settings
2. Navigate to "Pages" (under "Code and automation")
3. Under "Custom domain", enter `dr-syncer.io`
4. Check "Enforce HTTPS" to ensure secure connection
5. Save the settings

### 2. DNS Configuration

Configure your DNS provider with the following records for `dr-syncer.io`:

```
Type: A
Host: @
Value: 185.199.108.153
Value: 185.199.109.153
Value: 185.199.110.153
Value: 185.199.111.153
TTL: 1 hour (or as recommended by DNS provider)
```

And for the `www` subdomain:

```
Type: CNAME
Host: www
Value: supporttools.github.io
TTL: 1 hour (or as recommended by DNS provider)
```

### 3. Verify Setup

1. After the DNS changes propagate (can take up to 24 hours):
2. Visit https://dr-syncer.io to ensure the site loads correctly
3. Check that HTTPS is properly configured

## Local Development

```bash
# Navigate to the docs directory
cd docs

# Install dependencies
npm install

# Start the development server
npm start
```

Your browser should open to `http://localhost:3000`.

## Build

```bash
# Build the documentation
npm run build
```

This will create a `build` directory with the compiled static site.

## Deployment

The site is automatically deployed to GitHub Pages via the GitHub Actions workflow `.github/workflows/docs.yml` when changes are pushed to the `main` branch.

You can also manually trigger a deployment from the "Actions" tab in GitHub by selecting the "Deploy Documentation" workflow and clicking "Run workflow".
