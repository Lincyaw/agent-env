# Quick Start

Get up and running in 5 minutes!

## Basic Usage

### 1. Import the Library

```python
from yourproject import Client

# Create a client
client = Client(api_key="your-api-key")
```

### 2. Make Your First Request

```python
# Example: Fetch data
result = client.get_data(id=123)
print(result)
```

### 3. Handle Responses

```python
if result.success:
    print(f"Data: {result.data}")
else:
    print(f"Error: {result.error}")
```

## Common Use Cases

### Use Case 1: Basic Query

```python
from yourproject import Client

client = Client(api_key="your-api-key")
results = client.query(
    filters={"status": "active"},
    limit=10
)

for item in results:
    print(item.name)
```

### Use Case 2: Batch Operations

```python
# Process multiple items
items = [1, 2, 3, 4, 5]
results = client.batch_process(items)

print(f"Processed {len(results)} items")
```

### Use Case 3: Error Handling

```python
try:
    result = client.risky_operation()
except client.exceptions.NotFoundError:
    print("Resource not found")
except client.exceptions.AuthError:
    print("Authentication failed")
except Exception as e:
    print(f"Unexpected error: {e}")
```

## Configuration Options

### Environment Variables

Set up your environment:

```bash
export API_KEY="your-api-key"
export API_ENDPOINT="https://api.example.com"
export DEBUG=true
```

### Configuration File

Create `~/.yourproject/config.yml`:

```yaml
api_key: your-api-key
endpoint: https://api.example.com
timeout: 30
retry_attempts: 3
```

## Command-Line Interface

### Basic Commands

```bash
# Check version
yourproject --version

# Run with config file
yourproject --config config.yml run

# Enable verbose output
yourproject -v run
```

### Common Tasks

```bash
# Initialize new project
yourproject init my-project

# Run tests
yourproject test

# Deploy to production
yourproject deploy --env production
```

## What's Next?

Now that you have the basics:

- 📖 Read the full [User Guide](user-guide.md)
- 🔧 Learn about [Configuration](configuration.md)
- 📚 Explore the [API Reference](../api/overview.md)
- 💡 Check out more [Examples](../examples/overview.md)

## Interactive Tutorial

Try the interactive tutorial:

```bash
yourproject tutorial
```

This will guide you through common workflows step by step.

## Getting Help

!!! tip "Need help?"
    - Check the [FAQ](../faq.md)
    - Join our [Discord community](https://discord.gg/yourproject)
    - Open an issue on [GitHub](https://github.com/yourusername/yourproject/issues)
