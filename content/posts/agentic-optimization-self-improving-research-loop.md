---
title: "Agentic Optimization: Building a Self-Improving Research Loop"
date: 2026-06-22
slug: agentic-optimization-self-improving-research-loop
description: "Agentic optimization in practice: how AI agents, a strict evaluator, and an iteration ledger improved a byte-level language model from 1.871 to 1.604 BPB."
tags: [ai, agents, optimization, research]
draft: false
summary: "How an agentic optimization loop used rules, evals, and a ledger to make AI agents improve language-model experiments over six iterations."
---

I built an **agentic optimization** loop for language-model research.

AI agents proposed candidate models, a strict evaluator scored held-out bits per byte, and a research ledger turned each result into a better next brief. Over six iterations, the loop moved from a 1.871 BPB byte-GPT baseline to a replicated 1.604 BPB sparse MoE result.

The interesting part was not that an AI agent wrote a model file. It was that the system kept improving its own research process: better rules, better prompts, better rejection criteria, and better guesses about what to test next.

This is not recursive self-improving AGI. It is much smaller and more practical. It is a bounded research loop for language-model experiments, with a fixed compute budget, a held-out evaluation set, and one boring metric that every candidate has to face.

That boring part is the point.

## What Is Agentic Optimization?

There is a long research lineage behind self-improving systems. Rich Sutton's ["Bitter Lesson"](http://www.incompleteideas.net/IncIdeas/BitterLesson.html) argues that general methods powered by search and learning tend to win over hand-built cleverness. Jurgen Schmidhuber's [Godel Machine](https://people.idsia.ch/~juergen/goedelmachine.html) explored formal self-improvement. Jeff Clune's [AI-GAs](https://arxiv.org/abs/1905.10985) framed the idea of AI-generating algorithms, environments, and learning systems. More recently, projects like [The AI Scientist](https://arxiv.org/abs/2408.06292), [Darwin Godel Machine](https://arxiv.org/abs/2505.22954), and DeepMind's [AlphaEvolve](https://deepmind.google/blog/alphaevolve-a-gemini-powered-coding-agent-for-designing-advanced-algorithms/) have made the pattern feel newly concrete.

My version is intentionally modest: can agents run a serious empirical loop for improving a small byte-level language model?

The practical setup:

- agents design candidate models
- each candidate trains under the same token budget
- an evaluator scores held-out bits per byte, or BPB
- lower BPB means better next-byte prediction
- results go into a research ledger
- the next iteration starts from the ledger, not from intuition

![Diagram of the agentic optimization loop](/static/img/agentic-optimization-loop.svg)

*The loop is intentionally simple: agents can be creative because the evaluator is strict.*

## The Rules That Made Agents Useful

The first lesson was that agents are not automatically good researchers. They are energetic proposal generators. Without rules, they over-tune, chase noisy wins, invent explanations too early, and spend compute on experiments that are hard to compare.

So the real work was designing the game they were allowed to play.

The core rules were:

- **The orchestrator owns evaluation.** Candidate code can train and serve predictions, but it cannot choose the held-out set or grade itself.
- **Every candidate gets the same budget.** Training was fixed at 120 million tokens. Wall-clock time was only a runaway guard.
- **The metric is held-out BPB.** No custom success criteria, no hand-picked examples, no self-reported scores.
- **The held-out data stays held out.** Training used a loader that excluded the evaluation slice.
- **Noise gets measured before claims are believed.** A baseline byte-GPT was run across five seeds. Its mean BPB was 1.871 with sigma 0.0317.
- **Small wins do not count as discoveries.** A result had to clear the noise scale before we treated it as a real gain.
- **Every iteration writes down what happened.** Results, crashes, timeouts, surprises, and changed assumptions all went into the ledger.

That last rule mattered more than I expected. The ledger became the memory of the research system. It stopped the agents from rediscovering the same lesson every round.

Once those rules existed, the main agent could keep working: read the ledger, choose the next pressure points, spawn focused subagents, run candidates, score them, update the written rules, and repeat.

The human role changed from "pick the next model" to "design a loop where the next model choice has to be earned."

## How the Research Loop Worked

Each candidate lived in its own directory with a manifest, code, and notes. After training, it had to serve a small local API:

- health check
- log probabilities
- shutdown

The evaluator sent held-out bytes to the candidate and computed BPB itself. This contract made very different ideas comparable: n-grams, PPM-style compression models, GRUs, LSTMs, transformers, Mamba-style attempts, RWKV-style attempts, and sparse mixture-of-experts models all had to satisfy the same interface.

The experiment was small enough to run repeatedly, but strict enough to punish storytelling.

That combination is important. If the experiment is too open-ended, agents can produce impressive noise. If it is too narrow, they cannot explore. The useful middle ground was a sandbox where creativity happened before evaluation, not during evaluation.

## Outcomes Over Six Iterations

The first baseline was a small byte-level GPT: 6 layers, 384 dimensions, about 10.7 million parameters. It scored 1.871 BPB on the held-out set.

Then the agents started proposing alternatives.

![Line chart showing best BPB by iteration](/static/img/agentic-optimization-results.svg)

*Best known held-out BPB after each iteration. Lower is better.*

| Iteration | Best result in round | What happened |
| --- | ---: | --- |
| 0 | 1.871 | Baseline byte-GPT established the noise scale. |
| 1 | 1.879 | Classical and retrieval-heavy ideas did not beat the neural baseline. |
| 2 | 1.640 | A tuned modern transformer became the first major jump. |
| 3 | 1.638 | Dense transformer variants mostly tied the new plateau. |
| 4 | 1.629 | Training recipe changes helped, but only slightly. |
| 5 | 1.605 | Sparse MoE broke below the dense transformer floor. |
| 6 | 1.604 | A second full MoE run replicated the improvement. |

The biggest drop came from moving to a stronger modern transformer recipe. After that, dense transformer changes became much harder. Deeper, narrower, longer-context, and recipe-tuned variants clustered around the same region.

The next real improvement came from sparse mixture of experts. The best model, `iter6-moe-steps`, reached 1.6037 BPB. A previous full MoE configuration had reached 1.6052 BPB, close enough to make the result feel like a replicated pattern rather than a lucky run.

That distinction matters. A single run is a hint. Two independent configurations landing in the same region below the dense floor is evidence.

## Interesting Findings

The first finding was that a strict evaluator is more valuable than a clever prompt.

Prompting helped the agents behave, but evaluation changed what they could get away with. Claims stopped mattering. Scores mattered. A candidate either served log probabilities and improved BPB, or it did not.

The second finding was that agents need calibrated context.

In the first round, some subagents were effectively optimizing against a weak mental baseline. They produced plausible ideas, but the real baseline and sigma made those ideas less impressive. After that, the briefing changed: every agent received the current champion, the noise estimate, the budget, and known failure modes.

The third finding was that not all "different" ideas are useful diversity.

Classical n-gram and compression-style candidates were interesting, but at this budget they could not beat the neural baseline. Retrieval-style suffix ideas looked tempting and scored poorly. Some modern sequence-model attempts crashed, timed out, or produced NaNs. The loop made those failures useful because they narrowed the next search.

The fourth finding was that dense transformer tuning hit a local floor quickly.

Once the modern transformer recipe reached roughly 1.64 BPB, many plausible improvements became sub-sigma. Iteration 4 nudged the score to 1.6288 with training recipe changes, but it did not change the shape of the search. The system had learned that dense model polish was no longer the highest-leverage direction.

The fifth finding was that sparse capacity helped.

The MoE candidates had more total parameters, but only a subset was active per token. That made them a good fit for the constraint: more capacity without paying full dense cost on every token. The lean MoE lost much of the gain, which suggested the improvement was not just "add routing"; the full expert capacity mattered.

## How the Agents Improved Over Time

The most useful thing was not that an agent wrote a good model once.

The useful thing was that the loop became harder to fool over time.

Early on, the agents needed more correction. They proposed broad ideas, sometimes oversold weak signals, and occasionally spent effort in directions the evaluator could not reward. By later iterations, the system had a better playbook:

- compare against the current champion, not the original baseline
- treat sub-sigma changes as noise
- prefer experiments that test a specific hypothesis
- preserve failures because they teach the next prompt
- separate proposal generation from scoring
- only promote a candidate after the evaluator agrees

That is the practical version of self-improvement here. Not a model rewriting its own source code into superintelligence, but a research process improving its own instructions, memory, and search priorities.

## Why I Like "Agentic Optimization"

"AI agent" is too broad. "Automated research" is too grand. "Recursive self-improvement" suggests something much stronger than what happened.

**Agentic optimization** feels closer to the actual pattern:

- use agents to generate candidates
- use code to enforce the contract
- use data to decide winners
- use a ledger to carry learning forward
- repeat until the search stops paying rent

It is not magic. It is closer to a disciplined optimization loop where language models supply imagination and software supplies accountability.

That is also why this approach is interesting beyond language-model experiments. The same shape could apply to prompts, retrieval systems, data pipelines, compilers, model architectures, evaluation harnesses, or any domain where you can define a strict score and make candidates cheap enough to test.

## What I Would Improve Next

The loop worked, but it is still early.

The next version should have a sealed test slice that is used less often, so the research loop cannot gradually overfit the main held-out set. It should add a small out-of-distribution canary to catch candidates that only improve the narrow data distribution. It should make failure analysis more structured, especially for crashes and timeouts. It should also push agents to produce more differentiated hypotheses instead of variations on the same favorite recipe.

I would also make promotion stricter. A good rule of thumb is:

1. one seed to reject obvious losers
2. multiple seeds for plausible challengers
3. independent replication before calling something a new direction

That keeps the system fast when ideas are bad and careful when ideas look good.

## Takeaway

The main lesson is simple: agents become much more useful when they are placed inside a loop that remembers, measures, and refuses to be impressed by prose.

Agentic optimization is not about handing research to an AI and hoping it becomes brilliant. It is about building a research machine where agents create options, evaluators create pressure, and the ledger turns experience into the next round of better search.

For this experiment, that was enough to move from a 1.871 BPB baseline to a replicated 1.604 BPB MoE result.

The result is interesting. The loop is the part I want to keep building.
