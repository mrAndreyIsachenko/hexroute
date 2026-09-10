## MODIFIED Requirements

### Requirement: Installed policy configuration is checkable before installation

An operator SHALL be able to validate a prepared daemon policy configuration
offline, before it is installed, using the same validation the daemon applies
rather than a second statement of the rules.

The check SHALL also be able to judge a candidate against the configuration
already installed, and SHALL refuse one that drops any setting the installed
configuration carries. A configuration that removes an authority is not
malformed — it is a different, valid configuration — so validity alone cannot
be what stands between a stale file and a signed generation.

An installation SHALL keep the configuration it replaced, and a runtime whose
store holds an authority its configuration cannot read SHALL report that rather
than run as though nothing were there.

#### Scenario: A prepared configuration is malformed

- **WHEN** the offline check runs against a configuration the daemon would reject
- **THEN** it reports the rejection without the daemon being restarted

#### Scenario: A prepared configuration is sound

- **WHEN** the offline check accepts a configuration
- **THEN** the daemon accepts the same file, because both ask the same function

#### Scenario: A candidate drops a setting the installed configuration carries

- **WHEN** the check is given both a candidate and the installed configuration, and the candidate omits a setting the installed one has
- **THEN** it refuses and names what would be lost, whether or not the candidate is valid on its own

#### Scenario: A candidate only adds

- **WHEN** the candidate carries every setting the installed configuration has, and more
- **THEN** the check accepts it, because gaining a setting is not losing one

#### Scenario: The reduction is intended

- **WHEN** an operator declares that the reduction is deliberate
- **THEN** the installation proceeds, because stepping a domain back to no authority is a real operation

#### Scenario: Nothing is installed yet

- **WHEN** no configuration is in place
- **THEN** the installation proceeds without a comparison, because there is nothing to lose

#### Scenario: An installation replaces a configuration

- **WHEN** a configuration is replaced
- **THEN** the one it replaced is kept beside it, owned and masked as the installed one is

#### Scenario: A store holds an authority the configuration cannot read

- **WHEN** a runtime starts without the settings that would let it read a policy, and its store holds an active generation
- **THEN** it reports that an authority is present and unreadable, and does not report it as valid
