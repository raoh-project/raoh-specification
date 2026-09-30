# Patterns

The `pattern` operation takes a pattern: an expression of a regular language over Unicode scalar
values. A pattern denotes a set of strings, and the operation accepts a string exactly when the
whole of it is one of them. What a pattern denotes is defined here, not by any regular-expression
engine: an implementation reads the pattern and matches by this meaning, whatever its host's engine
would make of the same text. This is the pattern language of Souther
([specification, string patterns](https://github.com/souther-lang/souther/blob/develop/specification.adoc#string-patterns)),
so a pattern means the same set of strings in both.

A pattern denotes a set of strings and says nothing about how a match is found. A matcher's strategy,
greedy or reluctant, changes nothing, and a group captures nothing.

## What a pattern is built from

A pattern is built from these, and from nothing else.

- A character that is not one of `\ . [ ( ) { | ? * ^ $ +` stands for itself. A character past the
  basic plane is one character.
- An escape stands for one character: `\t`, `\n`, `\r`, `\f`, `\a` (U+0007), `\e` (U+001B); `\0`
  followed by one to three octal digits, up to 377; `\x` followed by two hex digits, or by hex digits
  in braces up to 10FFFF; `\u` followed by four hex digits, where a high surrogate's `\u` followed at
  once by a low one's is the one character the two encode. Hex digits are the ASCII ones, in either
  case. A backslash before a character that is neither a letter nor a digit stands for that
  character, so `\.` is a full stop and `\\` a backslash.
- `.` stands for every character but the five line terminators U+000A, U+000D, U+0085, U+2028 and
  U+2029.
- `\d` stands for U+0030 to U+0039; `\w` for U+0041 to U+005A, U+0061 to U+007A, U+0030 to U+0039
  and U+005F; `\s` for U+0020 and U+0009 to U+000D. `\D`, `\W` and `\S` stand for every character
  the small one does not. These are ASCII, and `\s` is not the whitespace `trim` and `nonBlank` read.
- A class `[...]` stands for the characters it lists: characters and escapes, shorthands, and runs
  `a-z` whose ends are one character each and whose low end is not above its high one. A run is over
  scalar values, so `[\x{D7FF}-\x{E000}]` holds the two characters at its ends and none between.
  `[^...]` stands for every character the class does not list, the line terminators included. A `-`
  first or last in a class stands for itself, and so does a `]` right after `[` or `[^`. A class
  lists at least one character.
- `AB` is a string of `A` followed by one of `B`; `A|B` is either; `(A)` and `(?:A)` are `A`. Either
  side of `|`, and a group, may be empty.
- `A?`, `A*`, `A+`, `A{n}`, `A{n,}` and `A{n,m}` are between the two counts of `A`, the second no
  smaller than the first. A `?` after any of them is allowed and changes nothing.
- `^` and `$` are the start and the end of the string. The whole string is matched, so one at the
  edge adds nothing: `^[0-9]{3}$` is `[0-9]{3}`. A `^` after something every string of which has a
  character leaves no string. Anywhere its answer would turn on the string matched, it is refused:
  after something that may or may not take a character (`(a|)^b`), in a repetition that is not of
  exactly one (`(^a)*`), and a `$` before something that must take one (`a$b`).

Matching is case-sensitive and compares scalar values; nothing is normalized or folded. Every string
a decoder gives is text of scalar values (see [input-model.md](input-model.md)), so no pattern needs
to say what a surrogate is.

## What is refused

Everything else is refused, each for a reason of its own. A back reference, `\1` to `\9` and `\k`,
can denote a set no regular language is. A group beginning `(?` other than `(?:` is not in the
grammar: a lookaround and a named group have no spelling here, and a flag group would change what a
class or a shorthand means for the rest of the pattern, which the language keeps fixed. A possessive
count, `++` or any other count followed by `+`, accepts what a matcher's walk leaves, which the
language does not describe. `\p` and `\P`, the boundaries `\b`, `\B`, `\A`, `\z`, `\Z`, `\G` and
`\R`, the quotation `\Q`...`\E`, a class inside a class and `&&`, and a backslash before any letter
not named above have no spelling in the grammar. An escape spelling half of a surrogate pair,
`\uD800` on its own or `\x{DC00}`, names a character no string holds. A count of more than
134217727 and groups nested more than 200 deep are refused too.

A form whose pattern is refused is not a decoder of the decoder language: the `pattern` operation
requires its argument to be a pattern (`requires` in `catalog/operations.json`), and a case that
writes another is rejected.

## Implementing it

An implementation matches by this meaning. A regular-expression engine can do that when what it is
given means the same set: Java's `java.util.regex` does, for every pattern of this language, when it
matches the whole string (`Matcher.matches`) with no flags, since its `.`, `\d`, `\w` and `\s` are
the sets above by default; an ECMAScript engine needs the `u` flag and a translation of `.` and of
the shorthands to explicit classes. A backtracking engine may take time that grows fast with the
input on some patterns; that is a property of the engine, not of the pattern's meaning, and an
implementation that runs untrusted patterns should not use one.
