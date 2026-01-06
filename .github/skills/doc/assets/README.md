# Documentation Skill Assets

This directory contains template files and assets for the documentation skill.

## MkDocs Templates

The `mkdocs-template/` directory contains example files for a complete documentation site:

### Configuration
- `mkdocs.yml` - Full-featured MkDocs configuration with Material theme
- `github-workflow-docs.yml` - GitHub Actions workflow for automated deployment

### Example Content
- `index.md` - Homepage template with examples of all major features
- `guides-installation.md` - Installation guide template
- `guides-quickstart.md` - Quick start guide template

## Usage

When initializing a new documentation project, these templates can be copied and customized:

```bash
# Copy template files to new project
cp -r assets/mkdocs-template/* /path/to/new/project/

# Or reference specific files
cp assets/mkdocs-template/mkdocs.yml /path/to/project/
```

## Template Features

The templates demonstrate:

- Modern Material Design theme with dark mode
- Full navigation setup
- Code highlighting and syntax examples
- Mermaid diagram integration
- Admonitions and callouts
- Tabbed content
- Task lists
- Search functionality
- Social links
- GitHub Actions integration

## Customization

After copying templates, customize:

1. Site metadata in `mkdocs.yml` (name, description, URL)
2. Repository links
3. Navigation structure
4. Color scheme (primary/accent colors)
5. Content in markdown files
6. Social links and footer

## File Organization

```
mkdocs-template/
├── mkdocs.yml                    # Main configuration
├── index.md                      # Homepage
├── guides-installation.md        # Installation guide
├── guides-quickstart.md          # Quick start guide
└── github-workflow-docs.yml      # CI/CD workflow
```

When using these templates, organize them in your project as:

```
project/
├── mkdocs.yml
├── docs/
│   ├── index.md
│   └── guides/
│       ├── installation.md
│       └── quickstart.md
└── .github/
    └── workflows/
        └── docs.yml
```
