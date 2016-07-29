// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Util;
using System;
using System.Threading;
using System.Threading.Tasks;
using Xunit;

namespace Paymetheus.Tests.Decred.Util
{
    public static class MutexTests
    {
        [Fact]
        public static void MutexProvidesExclusiveAccess()
        {
            var m = new Mutex<object>(null);
            int c = 0;

            var tasks = new Task[100];

            for (var i = 0; i < 100; i++)
            {
                tasks[i] = Task.Run(async () =>
                {
                    for (var j = 0; j < 100; j++)
                    {
                        using (var g = await m.LockAsync())
                        {
                            if (Interlocked.Increment(ref c) != 1)
                            {
                                throw new Exception("Counter was not 0 when lock was entered: another task is inside its critical section.");
                            }
                            if (Interlocked.Decrement(ref c) != 0)
                            {
                                throw new Exception("Counter changed while lock was entered: another task is inside its critical section.");
                            }
                        }

                    }
                });
            }

            Task.WaitAll(tasks);
        }
    }
}
