// .amp/plugins/ask-user-choice.ts
//
// See https://ampcode.com/manual#plugins for more information about Amp plugins.
import type { PluginAPI } from '@ampcode/plugin'

export default function (amp: PluginAPI) {
	amp.registerTool({
		name: 'ask_user_choice',
		description:
			'Present the user with a multiple choice question when there are several possible approaches and you need them to pick one. Use when you have 2-5 concrete options to choose from.',
		inputSchema: {
			type: 'object',
			properties: {
				question: { type: 'string', description: 'The question to ask the user' },
				options: {
					type: 'array',
					items: { type: 'string' },
					description: 'The options to choose from (2-5 items)',
				},
			},
			required: ['question', 'options'],
		},
		async execute(input, ctx) {
			const question = input.question as string
			const options = input.options as string[]
			const answer = await ctx.ui.select({
				title: question,
				options,
			})

			if (!answer) return 'User dismissed the question without choosing.'

			const index = options.indexOf(answer)
			if (index >= 0) {
				return `User selected option ${index + 1}: ${answer}`
			}
			return `User selected: ${answer}`
		},
	})
}
