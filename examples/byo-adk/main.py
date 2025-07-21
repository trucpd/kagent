import asyncio
import argparse
from kagent.adk import run_a2a_server
from agent import root_agent

def main():
    parser = argparse.ArgumentParser(
        description="BYO-ADK Agent - A sample agent using Google ADK that can roll dice and check prime numbers",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python main.py                    # Start the agent server
  python main.py --help            # Show this help message
        """
    )
    args = parser.parse_args()
    
    print("Starting agent server")
    asyncio.run(run_a2a_server(root_agent))


if __name__ == "__main__":
    main()
