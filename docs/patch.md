# ibm-agentid patch

Changes some properties of an object. To replace the whole object, use [`replace`](replace.md).

```text
ibm-agentid patch <resource> <id> -d <file|->
ibm-agentid patch <resource> --name <name> -d <file|->
ibm-agentid patch <resource> --template
```

## Flags

The same as [`replace`](replace.md#flags).

## Behavior

* The data holds only the properties to change, laid out as in the object itself. A property that the data leaves out is not touched.
* The CLI sends the data as it is and does not merge. What a `null`, a list or a nested object means is defined by the API of the resource. For example, a nested object may be merged property by property, and a list replaced as a whole. The help of each resource says what applies.
* Read-only properties and masked secrets (`********`) are ignored. If nothing else is left, the command fails with "nothing to change", before any sign-in.
* If the data has an `id`, it must match the object.
* The updated object is printed on stdout, and `Patched <resource> <id>` goes to stderr.

## Examples

```sh
ibm-agentid patch foo --name web -d - <<EOF
settings:
  retries: 5
EOF

ibm-agentid patch foo foo-1a2b3c -d change.yaml
```
