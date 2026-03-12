// Human is a CLI tool that enables AI agents to interact with issue trackers
// and project management tools as human developers would.
//
// It provides a unified interface across multiple issue trackers including
// Jira, GitHub, GitLab, Linear, Azure DevOps, and Shortcut. Additional
// integrations include Notion, Figma, and Amplitude.
//
// # Features
//
//   - One CLI for multiple trackers with JSON and Markdown output
//   - Claude Code skills for ticket analysis and implementation planning
//   - Definition of Ready checks for issue quality
//   - Notion workspace search and page reading
//   - Figma design browsing and component inspection
//   - Amplitude analytics querying
//
// # Usage
//
//	human list                    # List issues
//	human get TICKET-123          # Get issue details
//	human create --title "..."    # Create an issue
//	human transition TICKET-123   # Transition issue status
//	human statuses                # List available statuses
//
// For full usage details, run:
//
//	human --help
package main
