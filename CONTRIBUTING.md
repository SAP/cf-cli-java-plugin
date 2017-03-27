# Contributing

When contributing to this repository, please first discuss the change you wish to make via issue before making a change.

We have a [code of conduct](#code-of-conduct), please follow it in all your interactions with the project.

## Contribute Code

You are welcome to contribute code to the Cloud Foundry CLI Java Plugin in order to fix bugs or to implement new features.

There are three important things to know:

1. You must be aware of the Apache License (which describes contributions) and agree to the Contributors License Agreement (CLA).
   This is common practice in all major Open Source projects.
   To make this process as simple as possible, we are using the [CLA assistant](https://cla-assistant.io/) for individual contributions.
   CLA assistant is an open source tool that integrates with GitHub very well and enables a one-click-experience for accepting the CLA.
   For company contributors, [special rules apply](#company-contributors).
2. We set ourselves [requirements regarding code style and quality](#pull-request-process), and we kindly ask you to do the same with PRs.
3. Not all proposed contributions can be accepted.
   Some features may, for example, just fit a separate plugin better.
   The code must fit the overall direction of Cloud Foundry CLI Java Plugin and really improve it, so there should be some "bang for the byte".
   For most bug fixes this is a given, but it would be advisable to first discus new major features with the maintainers by opening an issue on the project.

### Pull Request Process

This a checklist of things to keep in your mind when opening pull requests for this project.

-1. Before pushing anything, validate your pull request with `go test`
0. Make sure you have signed the [Contributor License Agreement](#contributor-license-agreement)
1. Make sure any added dependency is licensed under Apache v2.0 license
2. Strive for very high unit-test coverage and favor testing productive code over mocks
   (mock in depth wherever possible)
3. Update the README.md with details of changes to the options

Pull requests will be tested and validated by maintainers. In case small changes are needed (e.g., correcting typos), the maintainers may fix those issues themselves.
In case of larger issues, you may be asked to apply modifications to your changes before the Pull Request can be merged.

### Contributor License Agreement

When you contribute (code, documentation, or anything else), you have to be aware that your contribution is covered by the same [Apache 2.0 License](http://www.apache.org/licenses/LICENSE-2.0) that is applied to the Cloud Foundry CLI Java Plugin itself.
Also, you need to agree to the Individual Contributor License Agreement, which can be [found here](https://gist.github.com/CLAassistant/bd1ea8ec8aa0357414e8).
(This applies to all contributors, including those contributing on behalf of a company).
If you agree to its content, you simply have to click on the link posted by the CLA assistant as a comment to the pull request.
Click it to check the CLA, then accept it on the following screen if you agree to it.
CLA assistant will save this decision for upcoming contributions and will notify you if there is any change to the CLA in the meantime.

#### Company Contributors

If employees of a company contribute code, in **addition** to the individual agreement above, there needs to be one company agreement submitted.
This is mainly for the protection of the contributing employees.

A company representative authorized to do so needs to download, fill, and print
the [Corporate Contributor License Agreement](docs/SAP%20Corporate%20Contributor%20License%20Agreement.pdf) form. Then either:

-   Scan it and e-mail it to [opensource@sap.com](mailto:opensource@sap.com)
-   Fax it to: +49 6227 78-45813
-   Send it by traditional letter to: *Industry Standards &amp; Open Source Team, Dietmar-Hopp-Allee 16, 69190 Walldorf, Germany*

The form contains a list of employees who are authorized to contribute on behalf of your company.
When this list changes, please let us know.

## Code of Conduct

### Our Pledge

In the interest of fostering an open and welcoming environment, we as
contributors and maintainers pledge to making participation in our project and
our community a harassment-free experience for everyone, regardless of age, body
size, disability, ethnicity, gender identity and expression, level of experience,
nationality, personal appearance, race, religion, or sexual identity and
orientation.

### Our Standards

Examples of behavior that contributes to creating a positive environment
include:

* Using welcoming and inclusive language
* Being respectful of differing viewpoints and experiences
* Gracefully accepting constructive criticism
* Focusing on what is best for the community
* Showing empathy towards other community members

Examples of unacceptable behavior by participants include:

* The use of sexualized language or imagery and unwelcome sexual attention or
advances
* Trolling, insulting/derogatory comments, and personal or political attacks
* Public or private harassment
* Publishing others' private information, such as a physical or electronic
  address, without explicit permission
* Other conduct which could reasonably be considered inappropriate in a
  professional setting

### Our Responsibilities

Project maintainers are responsible for clarifying the standards of acceptable
behavior and are expected to take appropriate and fair corrective action in
response to any instances of unacceptable behavior.

Project maintainers have the right and responsibility to remove, edit, or
reject comments, commits, code, wiki edits, issues, and other contributions
that are not aligned to this Code of Conduct, or to ban temporarily or
permanently any contributor for other behaviors that they deem inappropriate,
threatening, offensive, or harmful.

### Scope

This Code of Conduct applies both within project spaces and in public spaces
when an individual is representing the project or its community. Examples of
representing a project or community include using an official project e-mail
address, posting via an official social media account, or acting as an appointed
representative at an online or offline event. Representation of a project may be
further defined and clarified by project maintainers.

### Enforcement

Instances of abusive, harassing, or otherwise unacceptable behavior may be
reported by contacting the project team at [sap_cp_performance [at] sap.com](mailto:sap_cp_performance@sap.com). All
complaints will be reviewed and investigated and will result in a response that
is deemed necessary and appropriate to the circumstances. The project team is
obligated to maintain confidentiality with regard to the reporter of an incident.
Further details of specific enforcement policies may be posted separately.

Project maintainers who do not follow or enforce the Code of Conduct in good
faith may face temporary or permanent repercussions as determined by other
members of the project's leadership.

### Attribution

This Code of Conduct is adapted from the [Contributor Covenant][homepage], version 1.4,
available at [http://contributor-covenant.org/version/1/4][version]

[homepage]: http://contributor-covenant.org
[version]: http://contributor-covenant.org/version/1/4/
