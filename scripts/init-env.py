#!/usr/bin/env python3
"""Create local configuration once, preserving existing credentials."""
import os
from pathlib import Path
import secrets

root = Path(__file__).resolve().parent.parent
content = (root / ".env.example").read_text()
content = content.replace("POSTGRES_PASSWORD=\n", f"POSTGRES_PASSWORD={secrets.token_hex(24)}\n")
content = content.replace("JWT_SECRET=\n", f"JWT_SECRET={secrets.token_hex(32)}\n")
try:
    fd = os.open(root / ".env", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
except FileExistsError:
    print(".env already exists; kept existing configuration")
else:
    with os.fdopen(fd, "w") as output:
        output.write(content)
    print("Created .env with unique local credentials")
