import asyncio
from kagent.adk import run_a2a_server
from agent import root_agent

def main():
    print("Hello from byo-adk!")
    asyncio.run(run_a2a_server(root_agent))


if __name__ == "__main__":
    main()
