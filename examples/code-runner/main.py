"""Simple code runner using Pyro SDK.

Runs user-provided Python code in an isolated sandbox.

Usage:
    export PYRO_API_KEY=pk_...
    python main.py
"""

import asyncio
from pyro_sdk import Pyro


async def main():
    pyro = Pyro()

    # Check server health
    health = await pyro.health()
    print(f"Server: {health['status']}, active sandboxes: {health['active_sandboxes']}")

    # Create a sandbox with Python image
    async with await pyro.sandbox.create(image="python", timeout=300) as sb:
        print(f"Sandbox created: {sb.id}")

        # Run some Python code
        result = await sb.run("print('Hello from Pyro sandbox!')")
        print(f"Output: {result.stdout}")

        # Install a package and use it
        await sb.exec(["pip", "install", "-q", "requests"])
        result = await sb.run("""
import requests
resp = requests.get('https://httpbin.org/ip')
print(f"Your IP: {resp.json()['origin']}")
""")
        print(f"Output: {result.stdout}")

        # Write and read a file
        await sb.write_file("/app/hello.txt", "Hello from the host!")
        content = await sb.read_file("/app/hello.txt")
        print(f"File content: {content.decode()}")

    print("Sandbox destroyed. Done!")


if __name__ == "__main__":
    asyncio.run(main())
