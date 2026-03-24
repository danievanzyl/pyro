"""LangChain agent with Pyro sandbox tool.

Gives an LLM agent the ability to execute code in a sandboxed environment.

Usage:
    export PYRO_API_KEY=pk_...
    export OPENAI_API_KEY=sk-...
    pip install pyrovm-sdk langchain langchain-openai
    python main.py
"""

import asyncio
from pyro_sdk import Pyro


async def run_code_in_sandbox(code: str) -> str:
    """Tool function: run Python code in an isolated Pyro sandbox."""
    pyro = Pyro()
    async with await pyro.sandbox.create(image="python", timeout=120) as sb:
        result = await sb.run(code)
        output = result.stdout
        if result.stderr:
            output += f"\nSTDERR: {result.stderr}"
        if result.exit_code != 0:
            output += f"\nExit code: {result.exit_code}"
        return output


async def main():
    # Standalone demo without LangChain dependency
    print("Pyro Sandbox Tool Demo")
    print("=" * 40)

    # Simulate what an agent would do
    tasks = [
        "import sys; print(f'Python {sys.version}')",
        "import os; print(f'Running as: {os.getenv(\"USER\", \"root\")}')",
        "print(sum(range(1000)))",
    ]

    for code in tasks:
        print(f"\n> Running: {code}")
        output = await run_code_in_sandbox(code)
        print(f"  Result: {output.strip()}")

    print("\nAll tasks completed safely in isolated sandboxes!")


if __name__ == "__main__":
    asyncio.run(main())
