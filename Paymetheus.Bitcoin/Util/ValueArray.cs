// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;

namespace Paymetheus.Bitcoin.Util
{
    public static class ValueArray
    {
        public static bool ShallowEquals<T>(IList<T> a, IList<T> b)
            where T : struct, IEquatable<T>
        {
            if (a == b)
                return true;
            if (a == null || b == null)
                return false;
            if (a.Count != b.Count)
                return false;

            for (var i = 0; i < a.Count; ++i)
            {
                if (!a[i].Equals(b[i]))
                {
                    return false;
                }
            }

            return true;
        }

        public static void Zero<T>(IList<T> array)
            where T : struct
        {
            for (var i = 0; i < array.Count; ++i)
            {
                array[i] = default(T);
            }
        }
    }
}
