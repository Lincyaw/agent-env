#!/usr/bin/env python3
"""Fix auto-generated arl_client imports to use relative imports.

OpenAPI Generator creates absolute imports like 'from arl_client.xxx import yyy',
but since arl_client is a submodule of the arl package, these need to be
converted to relative imports.
"""

import re
import sys
from pathlib import Path


def fix_imports(file_path: Path) -> bool:
    """Convert absolute arl_client imports to relative imports.
    
    Returns:
        True if file was modified, False otherwise
    """
    content = file_path.read_text()
    original_content = content
    
    # Pattern 1: from arl_client.xxx import yyy -> from .xxx import yyy
    # But we need to handle the special case where it's in arl_client/__init__.py
    # In that case: from arl_client.xxx import yyy -> from .xxx import yyy
    
    # Pattern 2: import arl_client.models -> from . import models (if in same package)
    # or from .. import arl_client (if needed, but usually not)
    
    # Determine the relative import prefix based on file location
    # If file is in arl_client/, use "."
    # If file is in arl_client/api/, use ".."
    # If file is in arl_client/models/, use ".."
    
    parts = file_path.relative_to(file_path.parents[3]).parts  # relative to sdk/python/arl/
    
    if len(parts) < 3:  # Not in the expected location
        return False
    
    # parts should be like: ('arl', 'arl_client', 'api_client.py')
    # or ('arl', 'arl_client', 'api', 'default_api.py')
    # or ('arl', 'arl_client', 'models', 'sandbox.py')
    
    if 'arl_client' not in parts:
        return False
    
    arl_client_idx = parts.index('arl_client')
    depth = len(parts) - arl_client_idx - 2  # -1 for arl_client itself, -1 for the file
    
    if depth < 0:
        depth = 0
    
    # Build relative import prefix
    if depth == 0:
        # In arl_client/ directly
        rel_prefix = "."
    else:
        # In subdirectory like arl_client/api/ or arl_client/models/
        rel_prefix = "." * (depth + 1)
    
    # Pattern 1a: from arl_client import xxx (direct import from package)
    # This should become: from . import xxx
    def replace_direct_from_import(match: re.Match) -> str:
        imports = match.group(1)
        if depth == 0:
            # In arl_client/ directly
            return f'from . import {imports}'
        else:
            # In subdirectory
            rel_prefix = '.' * (depth + 1)
            return f'from {rel_prefix} import {imports}'
    
    content = re.sub(
        r'from arl_client import ([^\n]+)',
        replace_direct_from_import,
        content
    )
    
    # Pattern 1b: from arl_client.xxx.yyy import zzz
    # Count the dots needed: if in arl_client/api/, importing from arl_client.models needs ..models
    def replace_from_import(match: re.Match) -> str:
        module_path = match.group(1)
        imports = match.group(2)
        
        # Split the module path
        parts = module_path.split('.')
        
        if depth == 0:
            # In arl_client/ directly, importing from arl_client.xxx -> from .xxx
            new_path = '.' + '.'.join(parts)
        else:
            # In arl_client/subdir/, importing from arl_client.xxx -> from ..xxx
            new_path = '.' * (depth + 1) + '.'.join(parts)
        
        return f'from {new_path} import {imports}'
    
    content = re.sub(
        r'from arl_client\.([a-zA-Z0-9_.]+) import ([^\n]+)',
        replace_from_import,
        content
    )
    
    # Pattern 2: import arl_client.models -> from . import models (if at same level)
    # This is trickier, but usually these should become relative too
    def replace_import(match: re.Match) -> str:
        module_path = match.group(1)
        parts = module_path.split('.')
        
        if depth == 0:
            # In arl_client/ directly
            if len(parts) == 1:
                # import arl_client.models -> from . import models
                return f'from . import {parts[0]}'
            else:
                # import arl_client.api.default_api -> from .api import default_api
                return f'from .{".".join(parts[:-1])} import {parts[-1]}'
        else:
            # In subdirectory
            rel_prefix = '.' * (depth + 1)
            if len(parts) == 1:
                return f'from {rel_prefix} import {parts[0]}'
            else:
                return f'from {rel_prefix}{".".join(parts[:-1])} import {parts[-1]}'
    
    content = re.sub(
        r'import arl_client\.([a-zA-Z0-9_.]+)',
        replace_import,
        content
    )
    
    # Pattern 3: Fix getattr(arl_client.xxx, ...) to use local reference
    content = re.sub(
        r'getattr\(arl_client\.([a-zA-Z0-9_]+),',
        r'getattr(\1,',
        content
    )
    
    if content != original_content:
        file_path.write_text(content)
        return True
    
    return False


def main() -> None:
    """Main entry point."""
    if len(sys.argv) != 2:
        print("Usage: fix-arl-client-imports.py <arl_client_dir>", file=sys.stderr)
        sys.exit(1)
    
    arl_client_dir = Path(sys.argv[1])
    
    if not arl_client_dir.exists():
        print(f"✗ Directory not found: {arl_client_dir}", file=sys.stderr)
        sys.exit(1)
    
    if not arl_client_dir.is_dir():
        print(f"✗ Not a directory: {arl_client_dir}", file=sys.stderr)
        sys.exit(1)
    
    # Find all Python files
    py_files = list(arl_client_dir.rglob('*.py'))
    
    modified_count = 0
    for py_file in py_files:
        if fix_imports(py_file):
            print(f"✓ Fixed imports in {py_file.relative_to(arl_client_dir.parent.parent)}")
            modified_count += 1
    
    if modified_count == 0:
        print("✓ No files needed fixing")
    else:
        print(f"\n✓ Fixed imports in {modified_count} file(s)")


if __name__ == "__main__":
    main()
