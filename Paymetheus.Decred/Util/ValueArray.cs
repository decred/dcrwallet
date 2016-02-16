// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;

namespace Paymetheus.Decred.Util
{
    public static class ValueArray
    {
        public static bool ShallowEquals<T>(IList<T> a, IList<T> b)
            where T : struct, IEquatable<T>
        {
            if (a == null)
                throw new ArgumentNullException(nameof(a));
            if (b == null)
                throw new ArgumentNullException(nameof(b));

            if (a == b)
                return true;
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
            if (array == null)
                throw new ArgumentNullException(nameof(array));

            for (var i = 0; i < array.Count; ++i)
            {
                array[i] = default(T);
            }
        }
    }
}
