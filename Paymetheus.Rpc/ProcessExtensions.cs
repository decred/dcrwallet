// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Diagnostics;
using System.Threading.Tasks;

namespace Paymetheus.Rpc
{
    public static class ProcessExtensions
    {
        public static Task<int> WaitForExitAsync(this Process process)
        {
            var tcs = new TaskCompletionSource<int>();
            process.EnableRaisingEvents = true;
            process.Exited += (sender, args) => tcs.SetResult(process.ExitCode);

            return tcs.Task;
        }

        public static void KillIfExecuting(this Process process)
        {
            try
            {
                process.Kill();
            }
            catch (InvalidOperationException) { } // Ignore error if process is already closed.
        }
    }
}
