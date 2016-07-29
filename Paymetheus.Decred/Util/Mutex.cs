// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Threading;
using System.Threading.Tasks;

namespace Paymetheus.Decred.Util
{
    public sealed class Mutex<T> where T : class
    {
        readonly T _instance;
        readonly SemaphoreSlim _sem = new SemaphoreSlim(1);

        public Mutex(T instance)
        {
            _instance = instance;
        }

        public struct MutexGuard : IDisposable
        {
            public T Instance { get; }

            readonly SemaphoreSlim _sem;

            internal MutexGuard(T instance, SemaphoreSlim sem)
            {
                Instance = instance;
                _sem = sem;
            }

            public void Dispose()
            {
                _sem?.Release();
            }
        }

        public async Task<MutexGuard> LockAsync()
        {
            await _sem.WaitAsync();
            return new MutexGuard(_instance, _sem);
        }
    }
}
