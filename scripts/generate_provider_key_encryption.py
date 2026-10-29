#!/usr/bin/env python3
"""Print a base64-encoded 32-byte AES key for provider_key_encryption.txt."""

import base64
import os

print(base64.b64encode(os.urandom(32)).decode("ascii"))
