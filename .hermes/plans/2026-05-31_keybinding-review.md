# Plan: Keybinding Review — CSV Edits

## What you changed

| GoField | Was | → Your edit | Verdict |
|---------|-----|-------------|---------|
| Sync | `f` | `s` | ✓ "s" for sync |
| SyncAll | `F` | `ctrl s` | ✓ needs `ctrl+s` (with +) |
| MarkRead | `x` | `r` | ⚠ **conflict** with Reply |
| Forward | `w` | `f` | ✓ "f" for forward (Gmail muscle memory) |
| Summary | `s` | `v` | ✓ "v" for view summary |

## The `r` conflict

Both MarkRead and Reply are set to `r`:

```
MarkRead, r, r, mark read      ← line 12
Reply,    r, r, reply           ← line 20
```

In `handleMainKey`, both are checked with `keyMatches`. Bubble Tea will match the first one in the switch statement — Reply (line 1009) comes before MarkRead (line 947), so `r` would always Reply and never MarkRead.

Options:

**A) Keep `r` for Reply, use `x` or `f` for MarkRead**
- `r` = Reply (unchanged, every email client ever)
- `x` = MarkRead (was fine, `x` is "cross off")
- `f` = Forward (your pick)

**B) `r` for MarkRead, move Reply to `shift+r`**
- But `R` is "MarkAllRead" — `r` and `R` would be related actions (read one / read all), which is actually elegant
- Reply moves to… what? `ctrl+r` is RemoveAttach in compose, but free in main view. Or `@` — unusual but available.

**C) `r` for MarkRead, move Reply to `g`**  
- `g` is free and unused. Not mnemonic though.

## Recommendation: Option A

```
f = forward     (your pick — Gmail convention, good)
r = reply       (unchanged — universal email convention)
x = mark read   (unchanged — "x" is "cross off / done")
s = sync        (your pick)
v = AI summary  (your pick)
ctrl+s = sync all (your pick)
```

This is the least disruptive. Only 4 keys change from current, no conflicts, no muscle-memory breakage for Reply.

## CSV formatting fixes needed

Two mechanical issues in the CSV:

1. **Line 2 (Up)**: stray backtick at end, `k↑` has literal ↑ symbol instead of `k`. Should be `"k,up"`.
2. **Line 11 (SyncAll)**: `ctrl s` missing the `+`. Should be `"ctrl+s"`.

## Updated CSV (Option A applied)

```
GoField,Keys,HelpDisplay,Description,Context
Up,"k,up",k/↑,up,global
Down,"j,down",j/↓,down,global
Left,"h,left",h/←,move left,global
Right,"l,right",l/→,move right,global
NextPane,"tab,]",tab/],next pane,global
PrevPane,"shift+tab,[",shift+tab/[,prev pane,global
Enter,enter,enter,open,global
Back,esc,esc,back,global
Sync,s,s,sync mailbox,main
SyncAll,"F,ctrl+s","F/ctrl+s",sync all,main
MarkRead,x,x,mark read,main
MarkAllRead,R,R,mark all read,main
OpenBrowser,o,o,open in browser,main
NextLink,"ctrl+n,alt+n",ctrl+n,next link,content
PrevLink,"ctrl+p,alt+p",ctrl+p,prev link,content
Search,/,/,search,main
UnreadOnly,u,u,toggle unread only,main
Compose,c,c,compose,main
Reply,r,r,reply,main
Forward,f,f,forward,main
Archive,a,a,archive,main
Command,"p,:",p,command palette,global
AccountManager,m,m,accounts,global
ThemePicker,T,T,theme picker,global
Settings,S,S,settings,global
UpdateInstall,U,U,install update,main
UpdateIgnore,i,i,ignore update,main
Help,?,?,help,global
Quit,q,q,quit,global
Summary,v,v,AI summary,main
CopyText,C,C,copy text,summary
SaveMD,M,M,save as .md,summary
ContentSearch,ctrl+f,ctrl+f,find in content,content
ToggleQuote,z,z,toggle quoted text,content
ToggleHeaders,ctrl+e,ctrl+e,toggle full headers,content
SaveAttach,ctrl+d,ctrl+d,save attachments,content
Add,a,a,add account,account_manager
Edit,e,e,edit account,account_manager
Delete,d,d,delete,account_manager+main
SaveAccount,"ctrl+s,f2",ctrl+s,save account,account_manager
TestAccount,"ctrl+t,f5",ctrl+t,test account,account_manager
AttachFile,ctrl+a,ctrl+a,attach file,compose
GrammarCheck,ctrl+g,ctrl+g,AI grammar check,compose
RemoveAttach,ctrl+r,ctrl+r,remove attachment,compose
CycleSender,ctrl+u,ctrl+u,change sender,compose
Confirm,enter,enter,confirm,overlay
Cancel,esc,esc,cancel,overlay
Space," ",space,toggle select,main
SelectAll,A,A,select all,main
Yes,y,y,yes,overlay
No,n,n,no,overlay
Tab,tab,tab,next field,overlay
Backspace,backspace,backspace,delete,overlay
```

Only 4 changes from the current live bindings: Sync `f→s`, SyncAll adds `ctrl+s`, Forward `w→f`, Summary `s→v`. No `r` conflict. Reply stays `r`. MarkRead stays `x`.

Ready to apply when you confirm.
