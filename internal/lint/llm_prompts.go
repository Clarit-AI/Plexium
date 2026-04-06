package lint

const contradictionPrompt = `You are analyzing wiki documentation for contradictions.

Given two wiki pages, identify any contradictions between them.

Page 1 (%s):
%s

Page 2 (%s):
%s

List any contradictions found. For each contradiction, provide:
- A one-sentence description
- The specific conflicting statements

If no contradictions found, respond with "NONE".
Format: One contradiction per line, starting with "CONTRADICTION: "`

const conceptExtractionPrompt = `Analyze these wiki pages and identify concepts that are mentioned in 3 or more pages but don't have their own dedicated page.

Pages:
%s

List concepts that deserve their own page. One per line, starting with "CONCEPT: "
If none found, respond with "NONE".`

const crossRefPrompt = `Analyze these wiki pages and identify pairs that should cross-reference each other but don't.

%s

List missing cross-references. Format:
CROSSREF: [source-page] -> [target-page]: [reason]
If none found, respond with "NONE".`

const stalenessPrompt = `Analyze this wiki page and determine if its content appears semantically outdated.
Consider: Are the technologies/approaches described still current? Does the content reference deprecated patterns?

Page (%s):
%s

If the page appears outdated, respond with:
STALE: [description of what appears outdated] | CONFIDENCE: [high/medium/low]
If the page appears current, respond with "CURRENT".`
