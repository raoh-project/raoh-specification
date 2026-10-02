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
  character, so `\.` is a full stop and `\\` a backslash. A letter is a character of General_Category
  L (Lu, Ll, Lt, Lm or Lo) and a digit one of Nd, both of Unicode 18.0.0, whatever version the
  platform an implementation runs on has: `\²` (No) is a superscript two, while `\٣` (Nd) and
  `\꟝` (U+A7DD, Lu from Unicode 18.0.0) are refused.
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
or digit not named above have no spelling in the grammar. An escape spelling half of a surrogate pair,
`\uD800` on its own or `\x{DC00}`, names a character no string holds.

## Limits on an admissible pattern

A pattern is admitted only within three limits. They are not part of what a pattern means: a
pattern past one of them denotes a set of strings like any other. They bound what running a
pattern costs, and every implementation holds to the same numbers, counted from the text, so that
no implementation's way of running patterns decides which ones it takes.

| Limit | At most |
|-------|---------|
| A count written in `{n}`, `{n,}` or `{n,m}` | 134217727 |
| Groups nested one inside another | 200 |
| States, counted as below | 250000 |

The states of a pattern are what it comes to with its repetitions written out, counted on the
pattern as written:

- a character, an escape, `.`, a shorthand and a class count one each, and so do `^` and `$`;
- an empty pattern, group or alternative counts nothing;
- a sequence counts the sum of its parts, and a group what is inside it;
- a choice of n alternatives, written with n - 1 bars, counts one, plus one more than each
  alternative;
- `A{n,m}` counts m times `A`, plus one; `A{n}` is `A{n,n}` and `A?` is `A{0,1}`;
- `A{n,}` counts n + 1 times `A`, plus one; `A*` is `A{0,}` and `A+` is `A{1,}`;
- the pattern counts one more than what it is written as.

So `a{249998}` is 1 + 249998 + 1 = 250000 states and is admitted, `a{249999}` is not, and neither
is `(a{500}){500}`, which is 1 + 500 × 501 + 1. `(a|b)*` is 1 + (1 + 2 + 2) + 1 = 7. An anchor
counts one wherever it stands and whatever it comes to. The count is that of a machine with a state
to start in, one after each set of characters, one where a choice ends and one where each of its
alternatives begins, and one where each repetition ends. An implementation may run another machine
or none; it counts the same number all the same, and takes every pattern within the limits.

A form whose pattern is refused, or is past one of these limits, is not a decoder of the decoder
language: the `pattern` operation requires its argument to be a pattern this chapter admits
(`requires` in `catalog/operations.json`), and a case that writes another is rejected.

## Implementing it

An implementation matches by this meaning. A regular-expression engine can do that when what it is
given means the same set: Java's `java.util.regex` does, for every pattern of this language, when it
matches the whole string (`Matcher.matches`) with no flags, since its `.`, `\d`, `\w` and `\s` are
the sets above by default; an ECMAScript engine needs the `u` flag and a translation of `.` and of
the shorthands to explicit classes. A backtracking engine may take time that grows fast with the
input on some patterns; that is a property of the engine, not of the pattern's meaning, and an
implementation that runs untrusted input should not use one. The limits above are what a matcher
that reads each character once can hold to: a pattern within them is a machine of at most 250000
states, so a match takes time proportional to the input, with a bound on the work per character
that every pattern admitted shares.
