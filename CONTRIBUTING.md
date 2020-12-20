# Development Workflow
This document describes the standard workflows and terminology for developers at Zcash. It is intended to provide procedures that will allow users to contribute to the open-source code base. Below are common workflows users will encounter:

1. Fork lightwalletd Repository 
2. Create Branch
3. Make & Commit Changes
4. Create Pull Request
5. Discuss / Review PR
6. Deploy / Merge PR

Before continuing, please ensure you have an existing GitHub or GitLab account. If not, visit [GitHub](https://github.com) or [GitLab](https://gitlab.com) to create an account. 

## Fork Repository
This step assumes you are starting with a new GitHub/GitLab environment. If you have already forked the lightwalletd repository, please continue to [Create Branch] section. Otherwise, open up a terminal and issue the below commands:

Note: Please replace `your_username`, with your actual GitHub username

```bash
git clone git@github.com:your_username/lightwalletd.git
cd lightwalletd
git remote set-url origin git@github.com:your_username/lightwalletd.git
git remote add upstream git@github.com:zcash/lightwalletd.git
git remote set-url --push upstream DISABLED
git fetch upstream
git branch -u upstream/master master
```
After issuing the above commands, your `.git/config` file should look similar to the following:

```bash
[core]
	repositoryformatversion = 0
	filemode = true
	bare = false
	logallrefupdates = true
[remote "origin"]
	url = git@github.com:your_username/lightwalletd.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[branch "master"]
	remote = upstream
	merge = refs/heads/master
[remote "upstream"]
	url = git@github.com:zcash/lightalletd.git
	fetch = +refs/heads/*:refs/remotes/upstream/*
	pushurl = DISABLED
```
This setup provides a single cloned environment to develop for lightwalletd. There are alternative methods using multiple clones, but this document does not cover that process.

## Create Branch
While working on the lightwalletd project, you are going to have bugs, features, and ideas to work on. Branching exists to aid these different tasks while you write code. Below are some conventions of branching at Zcash:

1. `master` branch is **ALWAYS** deployable
2. Branch names **MUST** be descriptive:
	* General format: `issue#_short_description` 

To create a new branch (assuming you are in `lightwalletd` directory):

```bash
git checkout -b [new_branch_name]
```
Note: Even though you have created a new branch, until you `git push` this local branch, it will not show up in your lightwalletd fork on GitHub (e.g. https://github.com/your_username/lightwalletd)

To checkout an existing branch (assuming you are in `lightwalletd` directory):

```bash
git checkout [existing_branch_name]
```
If you are fixing a bug or implementing a new feature, you likely will want to create a new branch. If you are reviewing code or working on existing branches, you likely will checkout an existing branch. To view the list of current lightwalletd GitHub issues, click [here](https://github.com/zcash/lightwalletd/issues). 

## Make & Commit Changes
If you have created a new branch or checked out an existing one, it is time to make changes to your local source code. Below are some formalities for commits:

1. Commit messages **MUST** be clear
2. Commit messages **MUST** be descriptive
3. Commit messages **MUST** be clean (see squashing commits for details)

While continuing to do development on a branch, keep in mind that other approved commits are getting merged into `master`.  In order to ensure there are minimal to no merge conflicts, we need `rebase` with master.

If you are new to this process, please sanity check your remotes:

```
git remote -v
```
```bash
origin    git@github.com:your_username/lightwalletd.git (fetch)
origin    git@github.com:your_username/lightwalletd.git (push)
upstream    git@github.com:zcash/lightwalletd.git (fetch)
upstream    DISABLED (push)
```
This output should be consistent with your `.git/config`:

```bash
[branch "master"]
	remote = upstream
	merge = refs/heads/master
[remote "origin"]
	url = git@github.com:your_username/lightwalletd.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[remote "upstream"]
	url = git@github.com:zcash/lightwalletd.git
	fetch = +refs/heads/*:refs/remotes/upstream/*
	pushurl = DISABLED
```
Once you have confirmed your branch/remote is valid, issue the following commands (assumes you have **NO** existing uncommitted changes):

```bash
git fetch upstream
git rebase upstream/master
git push -f
```
If you have uncommitted changes, use `git stash` to preserve them:

```bash
git stash
git fetch upstream
git rebase upstream/master
git push -f
git stash pop
```
Using `git stash` allows you to temporarily store your changes while you rebase with `master`. Without this, you will rebase with master and lose your local changes.

Before committing changes, ensure your commit messages follow these guidelines:

1. Separate subject from body with a blank line
2. Limit the subject line to 50 characters
3. Capitalize the subject line
4. Do not end the subject line with a period
5. Wrap the body at 72 characters
6. Use the body to explain *what* and *why* vs. *how*

Once synced with `master`, let's commit our changes:

```bash
git add [files...] # default is all files, be careful not to add unintended files
git commit -m 'Message describing commit'
git push
```
Now that all the files changed have been committed, let's continue to Create Pull Request section.

## Create Pull Request
On your GitHub page (e.g. https://github.com/your_username/lightwalletd), you will notice a newly created banner containing your recent commit with a big green `Compare & pull request`. Click on it.

First, write a brief summary comment for your PR -- this first comment should be no more than a few lines because it ends up in the merge commit message. This comment should mention the issue number preceded by a hash symbol (for example, #2984).

Add a second comment if more explanation is needed. It's important to explain why this pull request should be accepted. State whether the proposed change fixes part of the problem or all of it; if the change is temporary (a workaround) or permanent; if the problem also exists upstream (Bitcoin) and, if so, if and how it was fixed there.

If you click on `Commits`, you should see the diff of that commit; it's advisable to verify it's what you expect. You can also click on the small plus signs that appear when you hover over the lines on either the left or right side and add a comment specific to that part of the code. This is very helpful, as you don't have to tell the reviewers (in a general comment) that you're referring to a certain line in a certain file.

Add comments **before** adding reviewers, otherwise they will get a separate email for each comment you add. Once you're happy with the documentation you've added to your PR, select reviewers along the right side. For a trivial change (like the example here), one reviewer is enough, but generally you should have at least two reviewers, at least one of whom should be experienced. It may be good to add one less experienced engineer as a learning experience for that person.

## Discuss / Review PR
In order to merge your PR with `master`, you will need to convince the reviewers of the intentions of your code. 

**IMPORTANT:** If your PR introduces code that does not have existing tests to ensure it operates gracefully, you **MUST** also create these tests to accompany your PR.

Reviewers will investigate your PR and provide feedback. Generally the comments are explicitly requesting code changes or clarifying implementations. Otherwise Reviewers will reply with PR terminology:

> **Concept ACK** - Agree with the idea and overall direction, but have neither reviewed nor tested the code changes.

> **utACK (untested ACK)**- Reviewed and agree with the code changes but haven't actually tested them.

> **Tested ACK** - Reviewed the code changes and have verified the functionality or bug fix.

> **ACK** - A loose ACK can be confusing. It's best to avoid them unless it's a documentation/comment only change in which case there is nothing to test/verify; therefore the tested/untested distinction is not there.

> **NACK** - Disagree with the code changes/concept. Should be accompanied by an explanation.

### Squashing Commits
Before your PR is accepted, you might be requested to squash your commits to clean up the logs. This can be done using the following approach:

```bash
git checkout branch_name
git rebase -i HEAD~4
```
The integer value after `~` represents the number of commits you would like to interactively rebase. You can pick a value that makes sense for your situation. A template will pop-up in your terminal requesting you to specify what commands you would like to do with each prior commit:

```bash
Commands:
 p, pick = use commit
 r, reword = use commit, but edit the commit message
 e, edit = use commit, but stop for amending
 s, squash = use commit, but meld into previous commit
 f, fixup = like "squash", but discard this commit's log message
 x, exec = run command (the rest of the line) using shell
```
Modify each line with the according command, followed by the hash of the commit. For example, if I wanted to squash my last 4 commits into the most recent commit for this PR:

```bash
p 1fc6c95 Final commit message
s 6b2481b Third commit message
s dd1475d Second commit message
s c619268  First commit message
```
```bash
git push origin branch-name --force
```

## Deploy / Merge PR
Once your PR/MR has been properly reviewed, it will be ran in the build pipeline to ensure it is valid to merge with master.

Sometimes there will be times when your PR is waiting for some portion of the above process. If you are requested to rebase your PR, in order to gracefully merge into `master`, please do the following:

```bash
git checkout branch_name
git fetch upstream
git rebase upstream/master
git push -f
```
