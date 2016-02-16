// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.
//
// This file contains code adapted from mscorlib under the following license to avoid
// a .NET 4.6 dependency:
//
// The MIT License (MIT)
//
// Copyright(c) Microsoft Corporation
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

using System;

namespace Paymetheus.Decred.Util
{
    public static class DateTimeOffsetExtras
    {
        const int DaysTo10000 = 3652059;
        const long TicksPerDay = (long)10000 * 1000 * 60 * 60 * 24;
        const long MinTicks = 0;
        const long MaxTicks = DaysTo10000 * TicksPerDay;
        const long UnixEpochTicks = 621355968000000000;
        const long UnixEpochSeconds = 62135596800;

        public static DateTimeOffset FromUnixTimeSeconds(long seconds)
        {
            const long MinSeconds = MinTicks / TimeSpan.TicksPerSecond - UnixEpochSeconds;
            const long MaxSeconds = MaxTicks / TimeSpan.TicksPerSecond - UnixEpochSeconds;

            if (seconds < MinSeconds || seconds > MaxSeconds)
            {
                throw new ArgumentOutOfRangeException(nameof(seconds));
            }

            long ticks = seconds * TimeSpan.TicksPerSecond + UnixEpochTicks;
            return new DateTimeOffset(ticks, TimeSpan.Zero);
        }
    }
}
