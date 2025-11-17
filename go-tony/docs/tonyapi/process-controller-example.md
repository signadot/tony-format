# Process Controller Example

This document demonstrates a systems-level controller that manages Unix processes - launching, tracking, and killing processes. The controller maintains a set of managed processes and exposes their information as part of the virtual document.

## Overview

The process controller (`proc-controller`) mounts at `/proc` and manages Unix processes. It can:
- **Launch processes** via mutations (PATCH)
- **Track launched processes** and their state
- **Kill processes** via mutations (PATCH)
- **Query process information** via queries (MATCH)
- **Monitor process changes** via subscriptions (WATCH)

The controller only tracks processes that it has launched, not all system processes.

## Virtual Document Structure

```tony
proc:
  processes:
    - id: "proc-1"  # Controller-assigned ID
      pid: 1234      # System PID (set when launched)
      name: "nginx"
      state: "running"  # running, stopped, exited, killed
      cmdline: ["nginx", "-g", "daemon off;"]
      cwd: "/var/www"
      environ:
        PATH: "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin"
        HOME: "/root"
      launchedAt: "2024-01-15T10:00:00Z"
      exitedAt: null
      exitCode: null
      stat:
        utime: 12345
        stime: 6789
        vsize: 1048576
        rss: 4096
        num_threads: 1
      fd:
        - fd: 0
          path: "/dev/null"
        - fd: 1
          path: "/var/log/nginx/access.log"
        - fd: 2
          path: "/var/log/nginx/error.log"
    - id: "proc-2"
      pid: 5678
      name: "worker"
      state: "running"
      cmdline: ["python", "worker.py"]
      cwd: "/app"
      launchedAt: "2024-01-15T10:05:00Z"
      exitedAt: null
      exitCode: null
      # ... more process info
```

## Controller Implementation

### Schema Contribution

When mounting, the controller provides its schema:

```tony
mount:
  controller: "proc-controller"
  path: "/proc"
  config:
    source: procfs
    refresh: 1s  # How often to refresh process list
  schema:
    define:
      Process:
        pid: .number
        ppid: .number
        name: .string
        state: .string
        uid: .number
        gid: .number
        cmdline: .array(.string)
        cwd: .string
        exe: .string
        environ: .sparsearray(.string)
        stat:
          utime: .number
          stime: .number
          cutime: .number
          cstime: .number
          starttime: .number
          vsize: .number
          rss: .number
          num_threads: .number
        fd: .array(.FileDescriptor)
        limits:
          max_open_files: .number
          max_processes: .number
      FileDescriptor:
        fd: .number
        path: .string
    accept:
      proc:
        processes: .array(.Process)
```

### Controller Code (Bash Example)

```bash
#!/bin/bash
# proc-controller

# Read mount config from stdin
read CONFIG

# Initialize diff-based backend (e.g., etcd)
BACKEND="etcd://backend:2379/proc"
init_backend "$BACKEND"

# Read all diffs from backend and reconstruct current state
CURRENT_STATE=$(backend_get_state "$BACKEND")

# Send initial state to document server (sync virtual document)
echo "null" > /tmp/base.tony
echo "$CURRENT_STATE" > /tmp/current.tony
INITIAL_DIFF=$(o diff /tmp/base.tony /tmp/current.tony)
echo "$INITIAL_DIFF"

# Function to read process info from /proc
read_process_info() {
    local pid=$1
    local proc_dir="/proc/$pid"
    
    if [ ! -d "$proc_dir" ]; then
        return 1
    fi
    
    # Read basic info
    local name=$(cat "$proc_dir/comm" 2>/dev/null || echo "")
    local state_char=$(cat "$proc_dir/stat" 2>/dev/null | awk '{print $3}' || echo "")
    # Convert state char to readable state
    case "$state_char" in
        R) local state="running" ;;
        S|D) local state="sleeping" ;;
        Z) local state="zombie" ;;
        T) local state="stopped" ;;
        *) local state="unknown" ;;
    esac
    
    local cwd=$(readlink "$proc_dir/cwd" 2>/dev/null || echo "")
    
    # Read cmdline
    local cmdline=$(cat "$proc_dir/cmdline" 2>/dev/null | tr '\0' '\n' | head -20 || echo "")
    
    # Read environment (sample first few)
    local environ=$(cat "$proc_dir/environ" 2>/dev/null | tr '\0' '\n' | head -10 || echo "")
    
    # Read stat info
    local stat_line=$(cat "$proc_dir/stat" 2>/dev/null || echo "")
    local utime=$(echo "$stat_line" | awk '{print $14}' || echo "0")
    local stime=$(echo "$stat_line" | awk '{print $15}' || echo "0")
    local vsize=$(cat "$proc_dir/statm" 2>/dev/null | awk '{print $1 * 4096}' || echo "0")
    local rss=$(cat "$proc_dir/statm" 2>/dev/null | awk '{print $2 * 4096}' || echo "0")
    local num_threads=$(echo "$stat_line" | awk '{print $20}' || echo "1")
    
    # Read file descriptors
    local fds=""
    if [ -d "$proc_dir/fd" ]; then
        for fd in "$proc_dir/fd"/*; do
            local fd_num=$(basename "$fd")
            local fd_path=$(readlink "$fd" 2>/dev/null || echo "")
            if [ -n "$fd_path" ]; then
                fds="${fds}    - fd: $fd_num\n      path: \"$fd_path\"\n"
            fi
        done
    fi
    
    # Output as Tony document fragment
    cat <<TONY
      pid: $pid
      name: "$name"
      state: "$state"
      cwd: "$cwd"
      cmdline:
$(echo "$cmdline" | sed 's/^/        - /')
      environ:
$(echo "$environ" | sed 's/^/        /' | head -5)
      stat:
        utime: $utime
        stime: $stime
        vsize: $vsize
        rss: $rss
        num_threads: $num_threads
      fd:
$(echo -e "$fds" | head -10)
TONY
}

# Function to check if process is still running
is_process_running() {
    local pid=$1
    [ -d "/proc/$pid" ] && kill -0 "$pid" 2>/dev/null
}

# Function to launch a process
launch_process() {
    local id=$1
    local cmdline="$2"
    local cwd="$3"
    local environ="$4"
    
    # Generate unique ID if not provided
    if [ -z "$id" ]; then
        id="proc-$(date +%s)-$$"
    fi
    
    # Set up environment
    local env_args=""
    if [ -n "$environ" ]; then
        while IFS='=' read -r key value; do
            env_args="$env_args $key=\"$value\""
        done <<< "$environ"
    fi
    
    # Change directory if specified
    local cd_cmd=""
    if [ -n "$cwd" ]; then
        cd_cmd="cd \"$cwd\" && "
    fi
    
    # Launch process in background
    eval "$cd_cmd $env_args $cmdline" > /dev/null 2>&1 &
    local pid=$!
    
    # Wait a moment to see if it started successfully
    sleep 0.1
    if ! kill -0 "$pid" 2>/dev/null; then
        echo "proc: !error" >&2
        echo "  code: launch_failed" >&2
        echo "  message: \"Failed to launch process\"" >&2
        return 1
    fi
    
    # Get process info
    local proc_info=$(read_process_info "$pid" 2>/dev/null)
    local launched_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    
    # Output new process document
    cat <<TONY
proc:
  processes:
    - id: "$id"
      pid: $pid
      launchedAt: "$launched_at"
      exitedAt: null
      exitCode: null
$proc_info
TONY
}

# Function to kill a process
kill_process() {
    local id=$1
    local signal=${2:-TERM}  # Default to TERM signal
    
    # Find process by ID in current state
    # This is simplified - in practice would query backend
    local pid=$(echo "$CURRENT_STATE" | grep -A 20 "id: \"$id\"" | grep "pid:" | awk '{print $2}')
    
    if [ -z "$pid" ]; then
        echo "proc: !error" >&2
        echo "  code: not_found" >&2
        echo "  message: \"Process $id not found\"" >&2
        return 1
    fi
    
    # Kill the process
    if kill -$signal "$pid" 2>/dev/null; then
        # Wait for process to exit
        local count=0
        while kill -0 "$pid" 2>/dev/null && [ $count -lt 10 ]; do
            sleep 0.1
            count=$((count + 1))
        done
        
        # Check exit code
        local exit_code=0
        if kill -0 "$pid" 2>/dev/null; then
            # Process still running, force kill
            kill -KILL "$pid" 2>/dev/null
            exit_code=137  # SIGKILL
        else
            # Process exited, get exit code
            wait "$pid" 2>/dev/null
            exit_code=$?
        fi
        
        local exited_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
        
        # Output update
        cat <<TONY
proc:
  processes:
    - id: "$id"
      state: !replace
        from: "running"
        to: "killed"
      exitedAt: "$exited_at"
      exitCode: $exit_code
TONY
    else
        echo "proc: !error" >&2
        echo "  code: kill_failed" >&2
        echo "  message: \"Failed to kill process $id\"" >&2
        return 1
    fi
}

# Function to update process state from /proc
update_process_states() {
    local state_doc="$1"
    local updated_doc="$state_doc"
    
    # Extract process IDs and PIDs from state
    echo "$state_doc" | grep -A 30 "id:" | while IFS= read -r line; do
        if echo "$line" | grep -q "id:"; then
            local id=$(echo "$line" | awk '{print $2}' | tr -d '"')
        elif echo "$line" | grep -q "pid:"; then
            local pid=$(echo "$line" | awk '{print $2}')
            if [ -n "$pid" ] && [ "$pid" != "null" ]; then
                if ! is_process_running "$pid"; then
                    # Process exited, update state
                    local exit_code=0
                    wait "$pid" 2>/dev/null
                    exit_code=$?
                    local exited_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
                    
                    # Update document (simplified - would use o patch in practice)
                    updated_doc=$(echo "$updated_doc" | sed "s/id: \"$id\"/id: \"$id\"\n      state: \"exited\"\n      exitedAt: \"$exited_at\"\n      exitCode: $exit_code/")
                else
                    # Update process stats
                    local proc_info=$(read_process_info "$pid" 2>/dev/null)
                    # Merge proc_info into updated_doc (simplified)
                fi
            fi
        fi
    done
    
    echo "$updated_doc"
}

# Main loop: read queries/diffs, apply to backend, write results
PREVIOUS_STATE="$CURRENT_STATE"
PROC_COUNTER=0

while IFS= read -r INPUT; do
    # Check if this is a query (MATCH) or mutation (PATCH)
    if echo "$INPUT" | grep -q "patch:"; then
        # This is a mutation (PATCH)
        # Parse the patch to see what operation to perform
        
        # Check for launch operation
        if echo "$INPUT" | grep -q "cmdline:"; then
            # Launch process
            local id=$(echo "$INPUT" | grep "id:" | head -1 | awk '{print $2}' | tr -d '"')
            local cmdline=$(echo "$INPUT" | grep -A 10 "cmdline:" | grep "  -" | sed 's/  - //' | tr '\n' ' ')
            local cwd=$(echo "$INPUT" | grep "cwd:" | awk '{print $2}' | tr -d '"')
            local environ=$(echo "$INPUT" | grep -A 10 "environ:")
            
            NEW_PROCESS=$(launch_process "$id" "$cmdline" "$cwd" "$environ")
            
            # Apply to backend
            echo "$CURRENT_STATE" > /tmp/current.tony
            echo "$NEW_PROCESS" > /tmp/new_proc.tony
            NEW_STATE=$(o patch /tmp/new_proc.tony /tmp/current.tony)
            backend_apply_diff "$BACKEND" "$NEW_PROCESS"
            
            CURRENT_STATE="$NEW_STATE"
            RESULT_DIFF="$NEW_PROCESS"
            
        # Check for kill operation
        elif echo "$INPUT" | grep -q "state:.*killed\|state:.*!replace.*killed"; then
            # Kill process
            local id=$(echo "$INPUT" | grep "id:" | head -1 | awk '{print $2}' | tr -d '"')
            local signal=$(echo "$INPUT" | grep "signal:" | awk '{print $2}' | tr -d '"' || echo "TERM")
            
            KILL_RESULT=$(kill_process "$id" "$signal")
            
            # Apply to backend
            echo "$CURRENT_STATE" > /tmp/current.tony
            echo "$KILL_RESULT" > /tmp/kill_result.tony
            NEW_STATE=$(o patch /tmp/kill_result.tony /tmp/current.tony)
            backend_apply_diff "$BACKEND" "$KILL_RESULT"
            
            CURRENT_STATE="$NEW_STATE"
            RESULT_DIFF="$KILL_RESULT"
        else
            # Unknown mutation
            echo "proc: !error"
            echo "  code: invalid_mutation"
            echo "  message: \"Unknown mutation operation\""
            continue
        fi
    else
        # This is a query (MATCH)
        # Update process states from /proc
        CURRENT_STATE=$(update_process_states "$CURRENT_STATE")
        
        # Filter based on query (simplified - would use o match in practice)
        RESULT_DIFF="$CURRENT_STATE"
    fi
    
    # Write result
    echo "$RESULT_DIFF"
    
    PREVIOUS_STATE="$CURRENT_STATE"
    
    # Sleep briefly
    sleep 0.5
done
```

## Example Queries

### Query: Get all managed processes

```tony
!apiop
path: /proc/processes
match: !trim
  id: null
  pid: null
  name: null
  state: null
```

**Response:**
```tony
proc:
  processes:
    - id: "proc-1"
      pid: 1234
      name: "nginx"
      state: "running"
    - id: "proc-2"
      pid: 5678
      name: "worker"
      state: "running"
```

### Query: Get specific process by ID

```tony
!apiop
path: /proc/processes
match: !trim
  id: "proc-1"
  pid: null
  name: null
  cmdline: null
  stat:
    rss: null
    vsize: null
```

**Response:**
```tony
proc:
  processes:
    - id: "proc-1"
      pid: 1234
      name: "nginx"
      cmdline:
        - "nginx"
        - "-g"
        - "daemon off;"
      stat:
        rss: 4096
        vsize: 1048576
```

### Query: Find processes by name

```tony
!apiop
path: /proc/processes
match: !trim
  name: "nginx"
  id: null
  pid: null
  state: null
```

**Response:**
```tony
proc:
  processes:
    - id: "proc-1"
      pid: 1234
      name: "nginx"
      state: "running"
```

### Query: Get running processes only

```tony
!apiop
path: /proc/processes
match: !trim
  state: "running"
  id: null
  pid: null
  name: null
```

**Response:**
```tony
proc:
  processes:
    - id: "proc-1"
      pid: 1234
      name: "nginx"
      state: "running"
    - id: "proc-2"
      pid: 5678
      name: "worker"
      state: "running"
```

### Query: Get processes with high memory usage

```tony
!apiop
path: /proc/processes
match: !trim
  stat:
    rss: !gt 10485760  # > 10MB
  id: null
  name: null
  stat:
    rss: null
```

**Response:**
```tony
proc:
  processes:
    - id: "proc-1"
      name: "nginx"
      stat:
        rss: 16777216
```

### Query: Get file descriptors for a process

```tony
!apiop
path: /proc/processes
match: !trim
  id: "proc-1"
  fd:
    fd: null
    path: null
```

**Response:**
```tony
proc:
  processes:
    - id: "proc-1"
      fd:
        - fd: 0
          path: "/dev/null"
        - fd: 1
          path: "/var/log/nginx/access.log"
        - fd: 2
          path: "/var/log/nginx/error.log"
```

## Example Mutations

### Mutation: Launch a process

```tony
!apiop
path: /proc/processes
match:
  id: "proc-nginx-1"
patch:
  id: "proc-nginx-1"
  name: "nginx"
  cmdline:
    - "nginx"
    - "-g"
    - "daemon off;"
  cwd: "/var/www"
  environ:
    PATH: "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin"
    HOME: "/root"
  state: "running"
```

**Response (diff of what was created):**
```tony
proc:
  processes:
    - id: "proc-nginx-1"
      pid: 1234
      name: "nginx"
      cmdline:
        - "nginx"
        - "-g"
        - "daemon off;"
      cwd: "/var/www"
      environ:
        PATH: "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin"
        HOME: "/root"
      state: "running"
      launchedAt: "2024-01-15T10:00:00Z"
      exitedAt: null
      exitCode: null
```

### Mutation: Launch process without specifying ID (auto-generate)

```tony
!apiop
path: /proc/processes
match:
  # No id specified - controller will generate one
patch:
  name: "worker"
  cmdline:
    - "python"
    - "worker.py"
  cwd: "/app"
  state: "running"
```

**Response:**
```tony
proc:
  processes:
    - id: "proc-1705315200-12345"  # Auto-generated ID
      pid: 5678
      name: "worker"
      cmdline:
        - "python"
        - "worker.py"
      cwd: "/app"
      state: "running"
      launchedAt: "2024-01-15T10:05:00Z"
```

### Mutation: Kill a process (graceful termination)

```tony
!apiop
path: /proc/processes
match:
  id: "proc-nginx-1"
patch:
  state: !replace
    from: "running"
    to: "killed"
```

**Response (diff of what changed):**
```tony
proc:
  processes:
    - id: "proc-nginx-1"
      state: !replace
        from: "running"
        to: "killed"
      exitedAt: "2024-01-15T10:10:00Z"
      exitCode: 0
```

### Mutation: Force kill a process (SIGKILL)

```tony
!apiop
path: /proc/processes
match:
  id: "proc-nginx-1"
patch:
  state: !replace
    from: "running"
    to: "killed"
  signal: "KILL"  # Force kill
```

**Response:**
```tony
proc:
  processes:
    - id: "proc-nginx-1"
      state: !replace
        from: "running"
        to: "killed"
      exitedAt: "2024-01-15T10:10:05Z"
      exitCode: 137  # SIGKILL exit code
```

### Mutation: Kill multiple processes by name

```tony
!apiop
path: /proc/processes
match: !all
  name: "nginx"
patch: !all
  state: !replace
    from: "running"
    to: "killed"
```

**Response:**
```tony
proc:
  processes:
    - id: "proc-nginx-1"
      state: !replace
        from: "running"
        to: "killed"
      exitedAt: "2024-01-15T10:10:00Z"
      exitCode: 0
    - id: "proc-nginx-2"
      state: !replace
        from: "running"
        to: "killed"
      exitedAt: "2024-01-15T10:10:00Z"
      exitCode: 0
```

## Example Subscriptions (WATCH)

### Watch: Monitor all managed processes

```http
WATCH /api/query HTTP/1.1
Content-Type: application/tony

!apiop
path: /proc/processes
match: !trim
  id: null
  pid: null
  name: null
  state: null
```

**Response stream:**
```tony
# Initial state
---
proc:
  processes:
    - id: "proc-1"
      pid: 1234
      name: "nginx"
      state: "running"
    - id: "proc-2"
      pid: 5678
      name: "worker"
      state: "running"

# When new process is launched
---
proc:
  processes:
    - id: "proc-3"
      pid: 9999
      name: "new-process"
      state: "running"
      launchedAt: "2024-01-15T10:15:00Z"

# When process state changes (e.g., memory usage updates)
---
proc:
  processes:
    - id: "proc-1"
      stat:
        rss: !replace
          from: 4096
          to: 8192

# When process is killed
---
proc:
  processes:
    - id: "proc-2"
      state: !replace
        from: "running"
        to: "killed"
      exitedAt: "2024-01-15T10:20:00Z"
      exitCode: 0

# When process exits naturally
---
proc:
  processes:
    - id: "proc-3"
      state: !replace
        from: "running"
        to: "exited"
      exitedAt: "2024-01-15T10:25:00Z"
      exitCode: 0
```

### Watch: Monitor specific process

```http
WATCH /api/query HTTP/1.1
Content-Type: application/tony

!apiop
path: /proc/processes
match: !trim
  id: "proc-1"
  stat:
    rss: null
    vsize: null
  state: null
```

**Response stream:**
```tony
# Initial state
---
proc:
  processes:
    - id: "proc-1"
      stat:
        rss: 4096
        vsize: 1048576
      state: "running"

# When memory usage changes
---
proc:
  processes:
    - id: "proc-1"
      stat:
        rss: !replace
          from: 4096
          to: 8192

# When process is killed
---
proc:
  processes:
    - id: "proc-1"
      state: !replace
        from: "running"
        to: "killed"
      exitedAt: "2024-01-15T10:20:00Z"
      exitCode: 0
```

### Watch: Monitor running processes only

```http
WATCH /api/query HTTP/1.1
Content-Type: application/tony

!apiop
path: /proc/processes
match: !trim
  state: "running"
  id: null
  name: null
  pid: null
```

**Response stream:**
```tony
# Initial state (only running processes)
---
proc:
  processes:
    - id: "proc-1"
      pid: 1234
      name: "nginx"
      state: "running"
    - id: "proc-2"
      pid: 5678
      name: "worker"
      state: "running"

# When new process is launched
---
proc:
  processes:
    - id: "proc-3"
      pid: 9999
      name: "new-process"
      state: "running"

# When a process exits (removed from running list)
---
proc:
  processes: !key(id)
    - !delete
      id: "proc-2"
```

## Controller Registration

### Mount Request

```http
MOUNT /.mount/proc HTTP/1.1
Content-Type: application/tony
Host: document-server:8080

mount:
  controller: "proc-controller"
  path: "/proc"
  config:
    source: procfs
    refresh: 1s
  schema:
    define:
      Process:
        pid: .number
        ppid: .number
        name: .string
        state: .string
        uid: .number
        gid: .number
        cmdline: .array(.string)
        cwd: .string
        exe: .string
        environ: .sparsearray(.string)
        stat:
          utime: .number
          stime: .number
          cutime: .number
          cstime: .number
          starttime: .number
          vsize: .number
          rss: .number
          num_threads: .number
        fd: .array(.FileDescriptor)
        limits:
          max_open_files: .number
          max_processes: .number
      FileDescriptor:
        fd: .number
        path: .string
    accept:
      proc:
        processes: .array(.Process)
```

### Mount Response

```http
HTTP/1.1 200 OK
Content-Type: application/tony

mount:
  accepted: true
  path: "/proc"
```

## Integration with Document Server

After mounting, the process controller becomes part of the unified virtual document:

```tony
# Unified virtual document now includes processes
users:
- id: "123"
    name: "Alice"

posts:
- id: "post1"
    title: "First Post"

proc:  # Mounted from proc-controller
  processes:
    - pid: 1234
      name: "nginx"
      state: "S"
    # ... more processes
```

Queries can combine data from multiple controllers:

```tony
# Query processes and user data in one request
!apiop
path: /proc/processes
match: !trim
  pid: null
  name: null
---
!apiop
path: /users
match: !trim
  id: null
  name: null
```

## Advanced Features

### Process Filtering by User

The controller could add computed fields or support filtering:

```tony
# Query processes owned by specific user
!apiop
path: /proc/processes
match: !trim
  uid: 1000
  pid: null
  name: null
```

### Process Statistics

The controller could expose aggregate statistics:

```tony
proc:
  stats:
    total_processes: 150
    running: 45
    sleeping: 100
    zombie: 5
    total_memory: 8589934592  # bytes
```

### Process Groups

The controller could compute process groups:

```tony
proc:
  process_groups:
    - pgid: 1234
      processes:
        - pid: 1234
          name: "nginx"
        - pid: 5678
          name: "nginx-worker"
```

## Implementation Notes

1. **Process Management**: The controller only tracks processes it has launched. It maintains a list of managed process IDs and their corresponding system PIDs.

2. **Process Lifecycle**:
   - **Launch**: Controller spawns process and tracks it with an ID
   - **Running**: Process is active, controller periodically updates stats from `/proc`
   - **Exited**: Process exits naturally (detected via `/proc` check)
   - **Killed**: Process terminated via mutation

3. **Performance**: 
   - Only scan `/proc` for managed processes (not all system processes)
   - Cache process information
   - Only refresh on demand or at configured intervals
   - Use efficient diff computation to only report changes

4. **Permissions**: The controller runs with appropriate permissions to launch and kill processes. It should respect system security policies.

5. **Real-time updates**: For WATCH subscriptions, the controller:
   - Monitors managed processes for state changes
   - Detects when processes exit (via `/proc` checks)
   - Emits diffs when processes are launched, killed, or exit

6. **Backend storage**: Process state is stored in backend diff log:
   - Launch events create new process entries
   - Kill events update process state
   - Exit events update process state
   - History/audit trail of all process lifecycle events

7. **Error handling**:
   - If process fails to launch, return error diff
   - If process not found when killing, return error diff
   - If `/proc` is not available, handle gracefully
   - If process disappears unexpectedly, mark as exited

8. **Process IDs**: 
   - Controller-assigned IDs (`id` field) are stable and unique
   - System PIDs (`pid` field) may change if process restarts
   - Use controller ID for tracking, system PID for operations

9. **Signal handling**: Support different kill signals (TERM, KILL, etc.) via mutation parameters.

## Benefits

1. **Unified interface**: Process management accessible via same API as application data
2. **Process lifecycle management**: Launch, track, and kill processes through mutations
3. **Real-time monitoring**: WATCH subscriptions enable real-time process monitoring
4. **Query flexibility**: Filter and select process information using Tony query syntax
5. **Composable**: Can combine process queries with application data queries
6. **Filesystem-like**: Familiar mental model (like Linux's `/proc` but via API, inspired by Plan 9's "everything is a file" philosophy)
7. **Audit trail**: All process lifecycle events stored in diff log for history/audit
8. **Controlled processes**: Only tracks processes launched through the controller, providing isolation and control
